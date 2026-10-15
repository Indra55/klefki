import * as c from './crypto.ts';
import {unlockRevision, type Store, type Head, type Stored} from './store.ts';
import type {Remote} from './sync.ts';

export const retention = 30 * 24 * 60 * 60 * 1000;
export type Deletion = {object:string;folder:boolean;files:string[];heads?:string[]};
type LocalDeletion = Deletion & {pending:boolean};
export type DeletionPreview = {revision:string;count:number;targets:Deletion[]};
type Page = {epoch:string;latest:number;enabled:boolean;changes:{sequence:number;deletion:Deletion}[]};
const request = <T>(r:IDBRequest<T>) => new Promise<T>((resolve,reject)=>{r.onsuccess=()=>resolve(r.result);r.onerror=()=>reject(Error('Local trash storage failed'));});
const root = (h:Head)=>h.metadata.type==='folder'&&h.metadata.tags.includes('klefki:system:trash');
const internal = (h:Head)=>h.metadata.tags.some(t=>['klefki:system:search-index','klefki:system:semantic-index','klefki:system:analysis-correction'].includes(t));
const fileID = (r:Stored['revision'])=>r.file_key?c.hex(c.unbase64(r.file_key).subarray(18,34)):undefined;
export function trashDates(heads:Head[]) {
 const byObject=new Map<string,Head[]>();for(const h of heads)byObject.set(h.revision.object,[...(byObject.get(h.revision.object)||[]),h]);
 function date(h:Head,seen=new Set<string>()):number {
  if(!h.metadata.parent||seen.has(h.revision.object))return 0;seen.add(h.revision.object);
  for(const p of byObject.get(h.metadata.parent)||[]){if(root(p))return h.metadata.trashed_at||0;const at=date(p,seen);if(at)return at;}
  return 0;
 }
 return new Map(heads.map(h=>[h.revision.id,date(h)]));
}
export function previewDeletion(heads:Head[],revision:string):DeletionPreview {
 const byObject=new Map<string,Head[]>();for(const h of heads)byObject.set(h.revision.object,[...(byObject.get(h.revision.object)||[]),h]);
 function trashed(h:Head,seen=new Set<string>()):boolean {
  if(!h.metadata.parent||seen.has(h.revision.object))return false;seen.add(h.revision.object);
  return (byObject.get(h.metadata.parent)||[]).some(p=>root(p)||trashed(p,new Set(seen)));
 }
 const selected=new Set(heads.filter(h=>(!revision||h.revision.id===revision)&&!root(h)&&!internal(h)&&trashed(h)).map(h=>h.revision.object));
 c.check(!revision||selected.size>0);
 for(let changed=true;changed;){changed=false;for(const h of heads)if(!selected.has(h.revision.object)&&h.metadata.parent&&selected.has(h.metadata.parent)){selected.add(h.revision.object);changed=true;}}
 const targets:Deletion[]=[...selected].sort().map(object=>{const hs=byObject.get(object)!;return {object,folder:hs.some(h=>h.metadata.type==='folder'),files:[],heads:hs.map(h=>h.revision.id).sort()};});
 return {revision,count:targets.length,targets};
}
export function sameTargets(a:DeletionPreview,b:DeletionPreview){return JSON.stringify(a.targets.map(d=>[d.object,d.folder,d.heads]))===JSON.stringify(b.targets.map(d=>[d.object,d.folder,d.heads]));}
export async function initializeTrash(store:Store,key:Uint8Array,now=Date.now()) {
 const heads=await store.heads(key),roots=new Set(heads.filter(root).map(h=>h.revision.object));
 for(const h of heads){if(h.conflict||root(h)||!h.metadata.parent||!roots.has(h.metadata.parent)||h.metadata.trashed_at)continue;
  let previous:string|null=null;
  if(h.revision.base){const old=unlockRevision(key,store.account.account,await store.revision(h.revision.base));previous=old.metadata.parent;if(old.key)c.wipe(old.key);}
  await store.edit(key,h.revision,{...h.metadata,trashed_at:now,trash_previous_parent:previous});
 }
}
export async function deletionRecords(store:Store,pending=false):Promise<LocalDeletion[]> {
 const all=await store.transaction(['deletions'],'readonly',tx=>request(tx.objectStore('deletions').getAll())) as LocalDeletion[];
 return all.filter(d=>!pending||d.pending);
}
export async function erase(store:Store,deletions:Deletion[],pending:boolean) {
 await store.transaction(['deletions','revisions','chunks'],'readwrite',async tx=>{
  const ledger=tx.objectStore('deletions'),revisions=tx.objectStore('revisions'),chunks=tx.objectStore('chunks'),all=await request(revisions.getAll()) as Stored[],files=new Set<string>();
  for(const input of deletions){c.validID(input.object);c.check(typeof input.folder==='boolean'&&Array.isArray(input.files));input.files.forEach(c.validID);
   const target=all.filter(x=>x.revision.object===input.object);
   if(pending&&input.heads){const parents=new Set(all.map(x=>x.revision.base));c.check(JSON.stringify(target.filter(x=>!parents.has(x.revision.id)).map(x=>x.revision.id).sort())===JSON.stringify([...input.heads].sort()));}
   const old=await request(ledger.get(input.object)) as LocalDeletion|undefined;
   const ids=[...new Set([...input.files,...(old?.files||[]),...target.map(x=>fileID(x.revision)).filter((f):f is string=>!!f)])].sort();ids.forEach(f=>files.add(f));
   ledger.put({object:input.object,folder:input.folder||old?.folder||false,files:ids,pending:pending||old?.pending||false} satisfies LocalDeletion);
   for(const x of target)revisions.delete(x.revision.id);
  }
  const remaining=await request(revisions.getAll()) as Stored[];for(const x of remaining){const f=fileID(x.revision);if(f)files.delete(f);}
  await new Promise<void>((resolve,reject)=>{const cursor=chunks.openCursor();cursor.onerror=()=>reject(Error('Chunk cleanup failed'));cursor.onsuccess=()=>{const row=cursor.result;if(!row){resolve();return;}if(files.has(String(row.key).split(':')[0]))row.delete();row.continue();};});
 });
 if(pending)store.onChange?.();
}
function references(value:unknown,ids:Set<string>):boolean {
 if(typeof value==='string')return ids.has(value);if(Array.isArray(value))return value.some(x=>references(x,ids));if(value&&typeof value==='object')return Object.values(value).some(x=>references(x,ids));return false;
}
export async function pruneDeleted(store:Store,key:Uint8Array) {
 const records=await deletionRecords(store);if(!records.length)return;
 const ids=new Set(records.flatMap(d=>[d.object,...d.files])),all=await store.all(),parents=new Set(all.map(x=>x.revision.base)),targets=new Map<string,Deletion>();
 for(let changed=true;changed;){changed=false;for(const x of all){if(ids.has(x.revision.object))continue;
  const {metadata,key:fk}=unlockRevision(key,store.account.account,x.revision);if(fk)c.wipe(fk);
  let remove=!parents.has(x.revision.id)&&!!metadata.parent&&ids.has(metadata.parent);
  const isInternal=internal({metadata,revision:x.revision,conflict:false});
  if(isInternal&&x.deferred)remove=true;
  if(isInternal&&!x.deferred){c.check((metadata.size||0)<=64*1024*1024);const bytes=await store.export(key,x.revision);try{try{remove ||= references(c.strictJSON(c.decode(bytes)),ids);}catch{remove=true;}}finally{c.wipe(bytes);}}
  if(remove){ids.add(x.revision.object);if(metadata.file_id)ids.add(metadata.file_id);targets.set(x.revision.object,{object:x.revision.object,folder:metadata.type==='folder',files:[]});changed=true;}
 }}
 if(targets.size)await erase(store,[...targets.values()],true);
}
export async function deleteTrash(store:Store,key:Uint8Array,preview:DeletionPreview) {
 c.check(sameTargets(preview,previewDeletion(await store.heads(key),preview.revision)));
 if(await store.state('sync-connected')&&!await store.state('deletion-supported'))throw Error('DELETION_UPDATE_REQUIRED');
 await erase(store,preview.targets,true);await pruneDeleted(store,key);
}
export async function syncDeletions(store:Store,key:Uint8Array,remote:Remote) {
 let cursor=await store.state<number>('deletion-cursor')||0,epoch=await store.state<string>('deletion-epoch');
 const pull=(after:number)=>remote.json<Page>('GET','/v1/sync/deletions?after='+after,undefined,24*1024*1024);
 let page:Page;
 try{page=await pull(cursor);}catch(e){if(e&&typeof e==='object'&&'status' in e&&[404,405].includes(Number(e.status))){await store.transaction(['state'],'readwrite',async tx=>{tx.objectStore('state').put(false,'deletion-supported');});if((await deletionRecords(store,true)).length)throw Error('DELETION_UPDATE_REQUIRED');return;}throw e;}
 c.validID(page.epoch);
 if(epoch!==page.epoch){cursor=0;await store.transaction(['deletions'],'readwrite',async tx=>{const s=tx.objectStore('deletions');for(const d of await request(s.getAll()) as LocalDeletion[])s.put({...d,pending:true});});page=await pull(0);}
 for(;;){c.check(Array.isArray(page.changes)&&page.changes.length<=16&&Number.isSafeInteger(page.latest)&&page.latest>=cursor);
  for(const change of page.changes){c.check(Number.isSafeInteger(change.sequence)&&change.sequence>cursor&&change.sequence<=page.latest);await erase(store,[change.deletion],false);cursor=change.sequence;
   await store.transaction(['state'],'readwrite',async tx=>{const s=tx.objectStore('state');s.put(cursor,'deletion-cursor');s.put(page.epoch,'deletion-epoch');s.put(true,'deletion-supported');});
  }
  if(page.changes.length<16)break;page=await pull(cursor);
 }
 await store.transaction(['state'],'readwrite',async tx=>{const s=tx.objectStore('state');s.put(cursor,'deletion-cursor');s.put(page.epoch,'deletion-epoch');s.put(true,'deletion-supported');});
 await pruneDeleted(store,key);
 const pending=await deletionRecords(store,true),batches:Deletion[][]=[];let batch:Deletion[]=[],size=32;
 for(const record of pending){for(let start=0;start<Math.max(1,record.files.length);start+=4096){const d:Deletion={object:record.object,folder:record.folder,files:record.files.slice(start,start+4096)},length=JSON.stringify(d).length;if(batch.length===256||size+length>1400000){batches.push(batch);batch=[];size=32;}batch.push(d);size+=length+1;}}
 if(batch.length)batches.push(batch);for(const deletions of batches)await remote.request('POST','/v1/sync/deletions',{deletions});
 for(const d of pending){
  await store.transaction(['deletions'],'readwrite',async tx=>{const s=tx.objectStore('deletions'),current=await request(s.get(d.object)) as LocalDeletion;c.check(current);if(JSON.stringify(current.files)===JSON.stringify(d.files))s.put({...current,pending:false});});
 }
}
export async function expiredTrash(store:Store,key:Uint8Array,now=Date.now()) {
 const heads=await store.heads(key),dates=trashDates(heads),out:DeletionPreview[]=[],covered=new Set<string>();
 const conflicted=(h:Head,seen=new Set<string>()):boolean=>{if(seen.has(h.revision.object)||h.conflict)return true;seen.add(h.revision.object);return !!h.metadata.parent&&heads.filter(p=>p.revision.object===h.metadata.parent).some(p=>conflicted(p,new Set(seen)));};
 for(const h of heads){const at=dates.get(h.revision.id)||0;if(at<=0||now-at<retention||covered.has(h.revision.object)||internal(h)||conflicted(h))continue;
  const p=previewDeletion(heads,h.revision.id);p.targets.forEach(d=>covered.add(d.object));if(p.targets.some(d=>d.heads!.length!==1))continue;out.push(p);
 }
 return out;
}
export async function cleanupTrash(store:Store,key:Uint8Array,remote?:Remote) {
 if(await store.state('sync-connected')&&!remote){await pruneDeleted(store,key);return;}
 if(remote&&!await store.state('deletion-supported'))return;
 await initializeTrash(store,key);
 const expired=await expiredTrash(store,key);
 for(const p of expired){
  if(remote){
   const heads=await store.heads(key),parents=new Map(heads.map(h=>[h.revision.object,h.metadata.parent]));
   const depth=(object:string)=>{let n=0;const seen=new Set<string>();while(parents.get(object)&&!seen.has(object)){seen.add(object);object=parents.get(object)!;n++;}return n;};
   const targets=[...p.targets].sort((a,b)=>depth(b.object)-depth(a.object)||a.object.localeCompare(b.object));
   for(let start=0;start<targets.length;start+=256){const batch=targets.slice(start,start+256);
    try{await remote.request('POST','/v1/sync/deletions',{deletions:batch});}catch(e){if(e&&typeof e==='object'&&'status' in e&&e.status===409)break;throw e;}
    await erase(store,batch.map(d=>({...d,heads:undefined})),false);
   }
  }else await deleteTrash(store,key,p);
 }

 await pruneDeleted(store,key);if(remote&&(expired.length||(await deletionRecords(store,true)).length))await syncDeletions(store,key,remote);
}
