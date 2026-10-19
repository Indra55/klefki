import * as c from './crypto.ts';
import { Store, unlockRevision, type Revision, type Stored, type Head } from './store.ts';
import { Remote, RelayWaiting, provenance, push, chunkWindows, startProgress } from './sync.ts';
import {syncDeletions} from './trash.ts';

export type TrashOverride='follow-vault'|'on'|'off';
export type ContentSettings={mode:string;epoch:string;selective_content?:boolean;trash_default?:boolean;trash_policy_version?:number;deletion_sequence?:number;content_version?:number};
type ContentState={id:string;available:boolean;requested:boolean};
type Change={sequence:number;revision:Revision};
export const unavailable='File contents are unavailable. Reconnect a device holding this file and enable Trash sync or restore it there.';

export async function trashPolicy(store:Store){
 const override=await store.state<TrashOverride>('trash-override')||'follow-vault',saved=await store.state<boolean>('trash-default'),enabled=saved===true;
 return {override,known:saved!==undefined,default:enabled,effective:override==='on'||override==='follow-vault'&&enabled};
}
export async function setTrashOverride(store:Store,value:TrashOverride){
 c.check(['follow-vault','on','off'].includes(value));await store.setState('trash-override',value);
}
export async function setTrashDefault(store:Store,remote:Remote,enabled:boolean){
 const settings=await remote.json<ContentSettings>('GET','/v1/sync/settings',undefined,1024);
 c.check(settings.selective_content);
 await remote.request('PUT','/v1/sync/trash-policy',{enabled},1024);
 const latest=await remote.json<ContentSettings>('GET','/v1/sync/settings',undefined,1024);
 await savePolicy(store,latest);
}
async function savePolicy(store:Store,settings:ContentSettings){
 c.check(Number.isSafeInteger(settings.trash_policy_version)&&settings.trash_policy_version!>0);
 await store.transaction(['state'],'readwrite',async tx=>{
  const state=tx.objectStore('state'),version=await new Promise<number>((resolve,reject)=>{const q=state.get('trash-policy-version');q.onsuccess=()=>resolve(q.result||0);q.onerror=()=>reject(q.error);});
  if(settings.trash_policy_version!>=version){state.put(settings.trash_default||false,'trash-default');state.put(settings.trash_policy_version,'trash-policy-version');}
 });
}
export function trashObjects(heads:Head[]){
 const byObject=new Map<string,Head[]>();for(const h of heads)byObject.set(h.revision.object,[...(byObject.get(h.revision.object)||[]),h]);
 function trashed(h:Head,seen=new Set<string>()):boolean{
  if(h.metadata.type==='folder'&&h.metadata.tags.includes('klefki:system:trash'))return true;
  if(!h.metadata.parent||seen.has(h.revision.object))return false;
  seen.add(h.revision.object);const parents=byObject.get(h.metadata.parent)||[];
  return parents.length>0&&parents.every(p=>trashed(p,new Set(seen)));
 }
 return new Map([...byObject].map(([object,branches])=>[object,branches.every(h=>trashed(h))]));
}
async function action(remote:Remote,action:string,ids:string[]){
 for(let start=0;start<ids.length;start+=256)await remote.request('POST','/v1/sync/content',{action,ids:ids.slice(start,start+256)},1024);
}
async function states(remote:Remote,ids:string[]){
 const result=new Map<string,ContentState>();
 for(let start=0;start<ids.length;start+=256){
  const batch=ids.slice(start,start+256),page=await remote.json<{states:ContentState[]}>('POST','/v1/sync/content/state',{ids:batch},32768);
  c.check(Array.isArray(page.states)&&page.states.length===batch.length);
  page.states.forEach((s,i)=>{c.check(s.id===batch[i]&&typeof s.available==='boolean'&&typeof s.requested==='boolean');result.set(s.id,s);});
 }
 return result;
}
async function fetchContent(store:Store,master:Uint8Array,remote:Remote,r:Revision,guard?:()=>Promise<void>){
 const {metadata:m,key}=unlockRevision(master,store.account.account,r);c.check(key);
 const chunks:Uint8Array[]=[];remote.report(startProgress('downloading',r,m.name,'download',m.size!,m.chunk_count!,await store.origin(r.id)));
 try{
  let total=0;
  await chunkWindows(m.chunk_count!,remote.signal,async(i,signal)=>{
   await guard?.();
   const b=await remote.request('GET',`/v1/blobs/${m.file_id}/chunks/${i}`,undefined,c.CHUNK+87,{signal});
   const plain=c.open(key,{kind:5,account:store.account.account,object:m.file_id!,index:i,final:i===m.chunk_count!-1},b);
   const size=plain.length;c.wipe(plain);c.check(size===Math.min(c.CHUNK,m.size!-i*c.CHUNK));return {b,size};
  },(i,{b,size})=>{chunks.push(b);total+=size;remote.report({...remote.progress,chunk:i+1,doneBytes:total});});
  c.check(total===m.size);remote.signal.throwIfAborted();
  await store.commit(r,chunks,m.file_id,false,undefined,undefined,false);
 }finally{c.wipe(key);}
 remote.phase('complete');
}
export async function ensureContent(store:Store,master:Uint8Array,remote:Remote,r:Revision){
 if(await store.contentAvailable(master,r))return;
 await syncDeletions(store,master,remote);
 // Register the request durably so reconnecting the source can supply it later.
 await store.contentFlags(r.id,{explicit:true,wanted:true,pending:true});
 await action(remote,'request',[r.id]);
 if(!(await states(remote,[r.id])).get(r.id)!.available)throw new RelayWaiting(unavailable);
 await fetchContent(store,master,remote,r);await action(remote,'received',[r.id]);
}
export async function syncSelective(store:Store,master:Uint8Array,remote:Remote,settings:ContentSettings){
 c.check(settings.selective_content&&Number.isSafeInteger(settings.trash_policy_version)&&settings.trash_policy_version!>0);
 remote.phase('checking');await store.bind(remote.origin);await syncDeletions(store,master,remote);
 const local=await store.relayState();
 const prepared=await remote.json<{mode:string;epoch:string;devices:string[];ids:string[]}>('POST','/v1/sync/relay/prepare',{...local,sequence:await store.state<number>('cursor')||0,ids:[]},16384);
 await store.saveRelay(prepared.mode,prepared.epoch,prepared.devices);remote.relayDevice=local.device;
 await remote.request('POST','/v1/sync/content/activate',undefined,1024);
 await savePolicy(store,settings);
 const history=(await store.all()).sort((a,b)=>a.order-b.order),localTrash=trashObjects(await store.heads(master,true)),policy=await trashPolicy(store);
 for(let start=0;start<history.length;start+=256){
  const batch=history.slice(start,start+256),ids=batch.map(x=>x.revision.id);
  const needed=await remote.json<{ids:string[]}>('POST','/v1/sync/relay/need-batch',{ids},16384);
  c.check(Array.isArray(needed.ids)&&new Set(needed.ids).size===needed.ids.length&&needed.ids.every(id=>ids.includes(id)));
  for(const x of batch){
   if(needed.ids.includes(x.revision.id)&&x.revision.file_key&&(!localTrash.get(x.revision.object)||policy.effective)&&await store.contentAvailable(master,x.revision))await store.contentFlags(x.revision.id,{pending:true,uploadPending:true});
   if(needed.ids.includes(x.revision.id))await remote.request('PUT','/v1/sync/revisions/'+x.revision.id,x.revision,1024,{origin:x.origin});await store.acknowledge(x.revision.id);}
 }
 let cursor=await store.state<number>('metadata-cursor')||0,through:number|undefined;
 for(;;){
  const page=await remote.json<{changes:Change[];latest:number}>('GET','/v1/sync/changes?after='+cursor+(through===undefined?'':'&through='+through));
  c.check(Array.isArray(page.changes)&&page.changes.length<=16&&Number.isSafeInteger(page.latest)&&page.latest>=cursor);through??=page.latest;if(!page.changes.length)break;
  const sources=await provenance(remote,cursor);
  for(const change of page.changes){
   c.check(Number.isSafeInteger(change.sequence)&&change.sequence>cursor&&change.sequence<=through);
   const deleted=await store.transaction(['deletions'],'readonly',tx=>new Promise<boolean>((resolve,reject)=>{const request=tx.objectStore('deletions').get(change.revision.object);request.onsuccess=()=>resolve(!!request.result);request.onerror=()=>reject(request.error);}));
   if(deleted){await store.setState('metadata-cursor',change.sequence);cursor=change.sequence;continue;}
   let r=change.revision;
   if(r.metadata===''){const local=await store.existing(r);if(!local)throw new RelayWaiting('Waiting for another linked device to supply metadata.');r=local;}
   const {metadata:m,key}=unlockRevision(master,store.account.account,r);if(key)c.wipe(key);
   const have=await store.contentAvailable(master,r);
   await store.commit(r,[],m.file_id,false,change.sequence,sources.get(r.id)?.origin,!have,true);cursor=change.sequence;
  }
  if(cursor>=through)break;
 }
 let failure:unknown;try{await transferContent(store,master,remote);}catch(e){failure=e;}
 const records=(await store.all()).filter(x=>x.sequence!==undefined).sort((a,b)=>a.sequence!-b.sequence!);
 const missing=records.find(x=>x.deferred&&x.wanted);
 const durable=missing?Math.max(0,...records.filter(x=>x.sequence!<missing.sequence!).map(x=>x.sequence!)):cursor;
 const receipt=Math.max(await store.state<number>('cursor')||0,durable);await store.setState('cursor',receipt);
 remote.phase('acknowledging');await remote.request('PUT','/v1/sync/cursor',{sequence:receipt},1024);
 if(failure)throw failure;
 await store.setState('content-version',settings.content_version||0);
}
async function transferContent(store:Store,master:Uint8Array,remote:Remote){
 const history=(await store.all()).sort((a,b)=>a.order-b.order),trash=trashObjects(await store.heads(master,true)),policy=await trashPolicy(store);
 const active:Stored[]=[],optional:Stored[]=[],requests:string[]=[],releases:string[]=[];
 for(const x of history){
  const r=x.revision;if(!r.file_key)continue;
  const have=await store.contentAvailable(master,r),wanted=!trash.get(r.object)||policy.effective||!!x.explicit;
  await store.contentFlags(r.id,{wanted,pending:wanted,trash:!!trash.get(r.object)});
  if(!wanted){await store.contentFlags(r.id,{pending:false,uploadPending:false});releases.push(r.id);continue;}
  if(!have)requests.push(r.id);
  (trash.get(r.object)&&!x.explicit?optional:active).push(x);
 }
 await action(remote,'release',releases);await action(remote,'request',requests);
 let waiting=false;
 for(const [priority,group] of [active,optional].entries()){
  const status=await states(remote,group.map(x=>x.revision.id));
  const uploads:string[]=[],received:string[]=[];
  for(const x of group){const s=status.get(x.revision.id)!;if(!s.available&&s.requested&&await store.contentAvailable(master,x.revision))uploads.push(x.revision.id);}
  await action(remote,'request',uploads);
  for(const x of group){
   const r=x.revision;remote.signal.throwIfAborted();
   if(trash.get(r.object)&&!x.explicit&&!(await trashPolicy(store)).effective)throw Error('CONTENT_REPLAN');
   let guard:(()=>Promise<void>)|undefined;
   if(priority===1){
    if((await store.all()).some(x=>x.pending&&!trash.get(x.revision.object)))throw Error('CONTENT_REPLAN');
    const next=await remote.json<{changes:Change[]}>('GET','/v1/sync/changes?after='+(await store.state<number>('metadata-cursor')||0));c.check(Array.isArray(next.changes));if(next.changes.length)throw Error('CONTENT_REPLAN');
    guard=async()=>{if(!(await trashPolicy(store)).effective)throw Error('CONTENT_REPLAN');};
   }
   const s=status.get(r.id)!;
   if(await store.contentAvailable(master,r)){
    if(!s.available&&s.requested){
     try{await push(store,master,remote,r,guard);}catch(e){if(e instanceof Error&&e.message==='CONTENT_REPLAN')await action(remote,'release',[r.id]);throw e;}
     await action(remote,'available',[r.id]);
    }
    received.push(r.id);await store.contentFlags(r.id,{pending:false,uploadPending:false});
   }else if(s.available){await fetchContent(store,master,remote,r,guard);received.push(r.id);}
   else waiting=true;
  }
  await action(remote,'received',received);
  if(waiting)throw new RelayWaiting(unavailable);
 }
 if(waiting)throw new RelayWaiting(unavailable);
}
