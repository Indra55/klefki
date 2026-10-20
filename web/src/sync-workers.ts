import * as c from './crypto.ts';
import {Store,unlockRevision,fileIdentity,workTask,type Stored,type Revision,type WorkTask,type DocumentEvent} from './store.ts';
import {Remote,HTTPError,RelayWaiting,provenance,push} from './sync.ts';
import {fetchContent,trashPolicy,type ContentSettings,unavailable} from './selective-sync.ts';
import {syncDeletions,deletionRecords} from './trash.ts';

const request=<T>(r:IDBRequest<T>)=>new Promise<T>((resolve,reject)=>{r.onsuccess=()=>resolve(r.result);r.onerror=()=>reject(r.error);});
type Check={epoch:string;mode:string;metadata:number;content:number;deletion:number;policy:number;inventory:number};
type Change={sequence:number;revision:Revision};
type Task=WorkTask&{record:Stored};
const replan=()=>Error('CONTENT_REPLAN');
const globalError=(e:unknown)=>e instanceof TypeError||e instanceof HTTPError&&[401,403,428,429].includes(e.status);
const allTasks=(s:Store)=>s.transaction(['work'],'readonly',tx=>request(tx.objectStore('work').getAll()) as Promise<WorkTask[]>);
async function check(s:Store,r:Remote){
 r.relayDevice=(await s.relayState()).device;
 const value=await r.json<Check>('GET','/v1/sync/check',undefined,2048);
 c.validID(value.epoch);c.check(['persistent','temporary-relay'].includes(value.mode));
 for(const key of ['metadata','content','deletion','policy','inventory'] as const)c.check(Number.isSafeInteger(value[key])&&value[key]>=0);
 c.check(value.policy>0);return value;
}
export async function queueNeeded(s:Store,r:Remote){
 if(!await s.state('work-initialized')||await s.state('work-reconcile'))return true;
 const local=await s.transaction(['work','revisions'],'readonly',async tx=>!!await request(tx.objectStore('work').index('retryAt').count(IDBKeyRange.upperBound(Date.now())))||!!await request(tx.objectStore('revisions').index('metadataQueued').count(1)));
 if(local||(await deletionRecords(s,true)).length)return true;
 const remote=await check(s,r);
 const [epoch,mode,metadata,content,deletion,policy,inventory]=await Promise.all(['relay-epoch','sync-mode','metadata-cursor','work-content-cursor','deletion-cursor','trash-policy-version','work-inventory'].map(key=>s.state(key)));
 return remote.epoch!==epoch||remote.mode!==mode||remote.metadata!==(metadata||0)||remote.content!==(content||0)||remote.deletion!==(deletion||0)||remote.policy!==(policy||0)||remote.inventory!==(inventory??-1);
}
export async function retryFailed(s:Store){
 await s.transaction(['work'],'readwrite',async tx=>{const work=tx.objectStore('work');for(const x of await request(work.getAll()) as WorkTask[])work.put({...x,direction:'plan',retryAt:0,error:''});});
}
async function initialize(s:Store){
 if(await s.state('work-initialized'))return;
 await s.transaction(['state','revisions','work'],'readwrite',async tx=>{
  const records=tx.objectStore('revisions'),work=tx.objectStore('work');
  for(const x of await request(records.getAll()) as Stored[]){
   const file=fileIdentity(x.revision);records.put({...x,file:file||undefined,metadataQueued:x.metadataPending||x.pending&&!x.deferred&&!x.uploadPending?1:0});
   if(file&&!await request(work.get(x.revision.id)))work.put(workTask(x.revision));
  }
  tx.objectStore('state').put(true,'work-initialized');
 });
}
async function publish(s:Store,r:Remote,record:Stored){
 const result=c.strictJSON(c.decode(await r.request('PUT','/v1/sync/revisions/'+record.revision.id,record.revision,1024,{origin:record.origin}))) as {sequence:number};
 c.check(Number.isSafeInteger(result.sequence)&&result.sequence>0);await s.acknowledge(record.revision.id);
}
async function inventory(s:Store,r:Remote){
 const history=(await s.all()).sort((a,b)=>a.order-b.order);
 for(let start=0;start<history.length;start+=256){
  const batch=history.slice(start,start+256),ids=batch.map(x=>x.revision.id);
  const needed=await r.json<{ids:string[]}>('POST','/v1/sync/relay/need-batch',{ids},16384);
  c.check(Array.isArray(needed.ids)&&new Set(needed.ids).size===needed.ids.length&&needed.ids.every(id=>ids.includes(id)));
  for(const x of batch)if(needed.ids.includes(x.revision.id))await publish(s,r,x);
 }
}
async function pullMetadata(s:Store,key:Uint8Array,r:Remote){
 let cursor=await s.state<number>('metadata-cursor')||0,through:number|undefined,changed=false;
 for(;;){
  const page=await r.json<{latest:number;changes:Change[]}>('GET','/v1/sync/changes?after='+cursor+(through===undefined?'':'&through='+through));
  c.check(Array.isArray(page.changes)&&page.changes.length<=16&&Number.isSafeInteger(page.latest)&&page.latest>=cursor);through??=page.latest;if(!page.changes.length)break;
  const sources=await provenance(r,cursor);
  for(const change of page.changes){
   changed=true;c.check(Number.isSafeInteger(change.sequence)&&change.sequence>cursor&&change.sequence<=through);
   const deleted=await s.transaction(['deletions'],'readonly',tx=>request(tx.objectStore('deletions').get(change.revision.object)));
   if(deleted){await s.setState('metadata-cursor',change.sequence);cursor=change.sequence;continue;}
   let revision=change.revision;
   if(revision.metadata===''){const local=await s.existing(revision);if(!local)throw new RelayWaiting('Waiting for another linked device to supply metadata.');revision=local;}
   const {metadata,key:fileKey}=unlockRevision(key,s.account.account,revision);if(fileKey)c.wipe(fileKey);
   const have=await s.contentAvailable(key,revision);
   await s.commit(revision,[],metadata.file_id,false,change.sequence,sources.get(revision.id)?.origin,!have,true);cursor=change.sequence;
  }
  if(cursor>=through)break;
 }
 await s.setState('work-baseline',true);if(changed)r.onSaved?.();
}
async function pullContent(s:Store,r:Remote){
 let through:number|undefined,cursor=await s.state<number>('work-content-cursor')||0;
 for(;;){
  const page=await r.json<{latest:number;changes:{sequence:number;file:string}[]}>('GET','/v1/sync/content/changes?after='+cursor+(through===undefined?'':'&through='+through),undefined,32768);
  c.check(Array.isArray(page.changes)&&page.changes.length<=256&&Number.isSafeInteger(page.latest)&&page.latest>=cursor);through??=page.latest;
  await s.transaction(['revisions','work','state'],'readwrite',async tx=>{
   const records=tx.objectStore('revisions'),work=tx.objectStore('work');
   for(const change of page.changes){
    c.validID(change.file);c.check(Number.isSafeInteger(change.sequence)&&change.sequence>cursor&&change.sequence<=through!);
    for(const x of await request(records.index('file').getAll(change.file)) as Stored[]){
     const old=await request(work.get(x.revision.id)) as WorkTask|undefined;
     if(!old)work.put(workTask(x.revision));else if(old.error==='source'||old.error==='capacity')work.put({...old,direction:'plan',retryAt:0,error:''});
    }
    cursor=change.sequence;
   }
   if(!page.changes.length)cursor=through!;
   tx.objectStore('state').put(cursor,'work-content-cursor');
  });
  if(cursor>=through)break;
 }
}
async function objectTrash(s:Store,key:Uint8Array,object:string,seen=new Set<string>()):Promise<boolean>{
 if(seen.has(object))return false;seen.add(object);
 const records=await s.transaction(['revisions'],'readonly',tx=>request(tx.objectStore('revisions').index('object').getAll(object)) as Promise<Stored[]>);
 const parents=new Set(records.map(x=>x.revision.base)),heads=records.filter(x=>!parents.has(x.revision.id));
 if(!heads.length)return false;
 for(const x of heads){
  const {metadata:m,key:fileKey}=unlockRevision(key,s.account.account,x.revision);if(fileKey)c.wipe(fileKey);
  if(m.type==='folder'&&m.tags.includes('klefki:system:trash'))continue;
  if(!m.parent||!await objectTrash(s,key,m.parent,new Set(seen)))return false;
 }
 return true;
}
async function ready(s:Store):Promise<Task[]>{
 return s.transaction(['work','revisions'],'readonly',async tx=>{
  const result:Task[]=[],records=tx.objectStore('revisions');
  for(const task of await request(tx.objectStore('work').index('retryAt').getAll(IDBKeyRange.upperBound(Date.now()))) as WorkTask[]){
   const record=await request(records.get(task.revision)) as Stored|undefined;if(record&&!record.metadataQueued&&!record.metadataPending)result.push({...task,record});
  }
  result.sort((a,b)=>a.priority-b.priority||(a.priority>=3?a.record.order-b.record.order:b.record.order-a.record.order));
  const files=new Set<string>();return result.filter(x=>{if(files.has(x.file))return false;files.add(x.file);return true});
 });
}
async function refs(s:Store,file:string){return s.transaction(['revisions'],'readonly',async tx=>(await request(tx.objectStore('revisions').index('file').getAll(file)) as Stored[]).filter(x=>!x.metadataQueued&&!x.metadataPending));}
async function action(r:Remote,action:string,ids:string[]){for(let start=0;start<ids.length;start+=256)await r.request('POST','/v1/sync/content',{action,ids:ids.slice(start,start+256)},1024);}
async function updateFile(s:Store,file:string,fn:(x:WorkTask)=>WorkTask|undefined){
 await s.transaction(['work'],'readwrite',async tx=>{const work=tx.objectStore('work');for(const x of await request(work.index('file').getAll(file)) as WorkTask[]){const next=fn(x);if(next)work.put(next);else work.delete(x.revision);}});
}
async function fail(s:Store,key:Uint8Array,r:Remote,t:Task,e:unknown){
 let error='transfer',retryAt=Date.now()+Math.min(5000*2**Math.min(t.attempts,6),300000)+Math.floor(Math.random()*1000);
 if(e instanceof RelayWaiting)error='source';
 if(e instanceof HTTPError&&e.status===507)error='capacity';
 if(e instanceof Error&&!(e instanceof HTTPError)&&!(e instanceof TypeError)&&!(e instanceof RelayWaiting)&&!(e instanceof DOMException)){error='integrity';retryAt=Number.MAX_SAFE_INTEGER;}
 await updateFile(s,t.file,x=>({...x,direction:'plan',attempts:x.attempts+1,retryAt,error,priority:x.priority<3?2:x.priority}));
 const {metadata,key:fileKey}=unlockRevision(key,s.account.account,t.record.revision);if(fileKey)c.wipe(fileKey);
 r.report({phase:'waiting',revision:t.revision,name:metadata.name,direction:t.direction==='receipt'||t.direction==='plan'?undefined:t.direction,attempt:t.attempts+1,retryAt,doneBytes:0,totalBytes:metadata.size||0,reusedBytes:0,chunk:0,chunks:metadata.chunk_count||0,updatedAt:Date.now()});
}
async function plan(s:Store,key:Uint8Array,r:Remote){
 const tasks=(await ready(s)).filter(x=>x.direction==='plan');
 const states=new Map<string,{available:boolean;requested:boolean}>();
 for(let start=0;start<tasks.length;start+=256){
  const batch=tasks.slice(start,start+256),ids=batch.map(x=>x.revision);
  const result=await r.json<{states:{id:string;available:boolean;requested:boolean}[]}>('POST','/v1/sync/content/state',{ids},32768);
  c.check(result.states.length===ids.length);result.states.forEach((x,i)=>{c.check(x.id===ids[i]&&typeof x.available==='boolean'&&typeof x.requested==='boolean');states.set(x.id,x);});
 }
 for(const t of tasks){
  const records=await refs(s,t.file),trash=await objectTrash(s,key,t.record.revision.object),policy=await trashPolicy(s),explicit=records.some(x=>x.explicit),wanted=!trash||policy.effective||explicit;
  for(const x of records)await s.contentFlags(x.revision.id,{wanted,trash,pending:wanted});
  const ids=records.map(x=>x.revision.id);
  if(!wanted){await action(r,'release',ids);await updateFile(s,t.file,()=>undefined);for(const x of records)await s.contentFlags(x.revision.id,{pending:false,uploadPending:false});continue;}
  const family=await s.transaction(['revisions'],'readonly',tx=>request(tx.objectStore('revisions').index('object').getAll(t.record.revision.object)) as Promise<Stored[]>);
  let priority=t.record.revision.base?1:0;if(t.attempts||family.some(x=>x.revision.base===t.revision))priority=2;if(trash&&!explicit)priority=3;
  const have=await s.contentAvailable(key,t.record.revision),state=states.get(t.revision)!;
  let direction:WorkTask['direction']='receipt';
  if(!have||!state.available&&state.requested){await action(r,'request',ids);direction=have?'upload':'download';}
  await updateFile(s,t.file,x=>({...x,direction,priority,uploadPending:direction==='upload'}));
  if(!have&&!state.available)await fail(s,key,r,{...t,priority},new RelayWaiting(unavailable));
 }
}
async function receipt(s:Store,r:Remote){
 const metadata=await s.state<number>('metadata-cursor')||0;
 let cursor=await s.state<number>('cursor')||0;
 await s.transaction(['revisions'],'readonly',tx=>new Promise<void>((resolve,reject)=>{
  const scan=tx.objectStore('revisions').index('sequence').openCursor(IDBKeyRange.lowerBound(cursor,true));scan.onerror=()=>reject(scan.error);
  scan.onsuccess=()=>{const row=scan.result;if(!row){cursor=metadata;resolve();return;}const record=row.value as Stored;if(record.deferred&&record.wanted){resolve();return;}cursor=record.sequence!;row.continue();};
 }));
 await s.setState('cursor',cursor);
 await r.request('PUT','/v1/sync/metadata-cursor',{sequence:metadata},1024);
 await r.request('PUT','/v1/sync/cursor',{sequence:cursor},1024);
}
export async function incoming(s:Store,key:Uint8Array):Promise<DocumentEvent[]>{
 const events=await s.transaction(['incoming','revisions'],'readonly',async tx=>{
  const out:DocumentEvent[]=[];
  for(const x of await request(tx.objectStore('incoming').getAll()) as (Omit<DocumentEvent,'name'|'stage'>&{reported:boolean})[]){
   const record=await request(tx.objectStore('revisions').get(x.revision)) as Stored|undefined;
   if(record&&!record.deferred){const {metadata,key:fileKey}=unlockRevision(key,s.account.account,record.revision);if(fileKey)c.wipe(fileKey);out.push({...x,name:metadata.name,stage:'ready'});}
  }
  return out;
 });
 return events.sort((a,b)=>b.created-a.created).slice(0,100);
}
export async function readIncoming(s:Store){await s.transaction(['incoming','revisions'],'readwrite',async tx=>{const records=tx.objectStore('incoming');for(const x of await request(records.getAll())){const record=await request(tx.objectStore('revisions').get(x.revision)) as Stored|undefined;if(record&&!record.deferred)records.put({...x,read:true});}});}
async function announce(s:Store,key:Uint8Array,r:Remote){
 for(const event of await incoming(s,key)){
  const fresh=await s.transaction(['incoming'],'readwrite',async tx=>{const records=tx.objectStore('incoming'),x=await request(records.get(event.revision));if(!x||x.reported)return false;records.put({...x,reported:true});return true;});
  if(fresh)r.onDocument?.(event);
 }
}
async function cycle(s:Store,key:Uint8Array,r:Remote){
 await syncDeletions(s,key,r);const state=await check(s,r);
 c.check(state.epoch===await s.state('relay-epoch'));if(state.mode!==await s.state('sync-mode'))throw replan();
 await initialize(s);
 const inventoryChanged=state.inventory!==(await s.state<number>('work-inventory')??-1);
 if(inventoryChanged)await inventory(s,r);
 if(state.policy!==await s.state('trash-policy-version')){
  const settings=await r.json<ContentSettings>('GET','/v1/sync/settings',undefined,1024);await s.setState('trash-default',!!settings.trash_default);await s.setState('trash-policy-version',settings.trash_policy_version);await s.setState('work-reconcile',true);
 }
 const pending=await s.transaction(['revisions'],'readonly',tx=>request(tx.objectStore('revisions').index('metadataQueued').getAll(1)) as Promise<Stored[]>);
 pending.sort((a,b)=>Number(!!a.revision.base)-Number(!!b.revision.base)||a.order-b.order);for(const x of pending)await publish(s,r,x);
 await pullMetadata(s,key,r);
 if(inventoryChanged||await s.state('work-reconcile')){
  await s.transaction(['work','revisions','state'],'readwrite',async tx=>{
   const work=tx.objectStore('work');for(const record of await request(tx.objectStore('revisions').getAll()) as Stored[]){if(!record.file)continue;const old=await request(work.get(record.revision.id)) as WorkTask|undefined;if(!old)work.put(workTask(record.revision));else if(!old.retryAt||old.error==='capacity')work.put({...old,direction:'plan',retryAt:0,error:''});}
   tx.objectStore('state').put(false,'work-reconcile');tx.objectStore('state').put(state.inventory,'work-inventory');
  });
 }
 await pullContent(s,r);await plan(s,key,r);await receipt(s,r);await announce(s,key,r);
}
async function perform(s:Store,key:Uint8Array,r:Remote,t:Task){
 const guard=async()=>{
  const deleted=await s.transaction(['deletions'],'readonly',tx=>request(tx.objectStore('deletions').get(t.record.revision.object)));if(deleted)throw replan();
  if(await objectTrash(s,key,t.record.revision.object)&&!(await trashPolicy(s)).effective&&!(await refs(s,t.file)).some(x=>x.explicit))throw replan();
  const metadata=await s.transaction(['revisions'],'readonly',tx=>request(tx.objectStore('revisions').index('metadataQueued').count(1)));
  if(metadata||(await ready(s)).some(x=>x.file!==t.file&&x.priority<t.priority))throw replan();
 };
 if(t.direction==='upload'){await push(s,key,r,t.record.revision,guard);await action(r,'available',[t.revision]);}
 if(t.direction==='download'){await fetchContent(s,key,r,t.record.revision,guard);r.onSaved?.();await announce(s,key,r);}
 const records=await refs(s,t.file);await action(r,'received',records.map(x=>x.revision.id));
 await s.transaction(['work','revisions'],'readwrite',async tx=>{
  const work=tx.objectStore('work'),revisions=tx.objectStore('revisions');
  for(const x of await request(revisions.index('file').getAll(t.file)) as Stored[])if(!x.metadataQueued&&!x.metadataPending){work.delete(x.revision.id);revisions.put({...x,pending:false,uploadPending:false});}
 });
}
export async function syncQueue(s:Store,key:Uint8Array,r:Remote,settings:ContentSettings){
 if(await s.state('work-active')&&settings.epoch===await s.state('relay-epoch')&&settings.mode===await s.state('sync-mode')&&!await queueNeeded(s,r))return;
 const local=await s.relayState();r.relayDevice=local.device;
 const prepared=await r.json<{mode:string;epoch:string;devices:string[];ids:string[]}>('POST','/v1/sync/relay/prepare',{...local,sequence:await s.state<number>('cursor')||0,metadataSequence:await s.state<number>('metadata-cursor')||0,ids:[]},16384);
 await s.saveRelay(prepared.mode,prepared.epoch,prepared.devices);await r.request('POST','/v1/sync/content/activate',undefined,1024);await s.setState('work-active',true);
 if((await s.state<number>('trash-policy-version')||0)!==settings.trash_policy_version){await s.setState('trash-default',!!settings.trash_default);await s.setState('trash-policy-version',settings.trash_policy_version);await s.setState('work-reconcile',true);}
 const running=new Map<string,{task:Task;controller:AbortController;promise:Promise<{task:Task;error?:unknown}>}>();
 let first:unknown,needsCycle=true,lastCycle=Date.now();
 try{
  for(;;){
   r.signal.throwIfAborted();
   if(needsCycle){await cycle(s,key,r);needsCycle=false;lastCycle=Date.now();}
   const tasks=await ready(s);
   for(const task of tasks){
    if(running.has(task.file))continue;
    if([...running.values()].some(x=>x.task.priority<task.priority))continue;
    if(running.size>=2){const worst=[...running.values()].filter(x=>x.task.priority>task.priority).sort((a,b)=>b.task.priority-a.task.priority)[0];worst?.controller.abort(replan());break;}
    if(task.priority>=3&&running.size)continue;
    const controller=new AbortController(),worker=r.fork(controller.signal);
    const promise=perform(s,key,worker,task).then(()=>({task})).catch(error=>({task,error:controller.signal.aborted?controller.signal.reason:error}));running.set(task.file,{task,controller,promise});
   }
   if(!running.size){await receipt(s,r);if(first)throw first;if((await allTasks(s)).length)throw new RelayWaiting(unavailable);return;}
   let timer:ReturnType<typeof setTimeout>|undefined;
   const waiting:Promise<{task:Task;error?:unknown}|undefined>[]=[...running.values()].map(x=>x.promise);
   waiting.push(new Promise(resolve=>{timer=setTimeout(()=>resolve(undefined),Math.max(0,5000-(Date.now()-lastCycle)));}));
   const result=await Promise.race(waiting);
   clearTimeout(timer);
   if(!result){needsCycle=true;continue;}
   running.delete(result.task.file);
   if(result.error){
    if(result.error instanceof Error&&result.error.message==='CONTENT_REPLAN')await updateFile(s,result.task.file,x=>({...x,direction:'plan'}));
    else{await fail(s,key,r,result.task,result.error);first??=result.error;if(globalError(result.error))throw result.error;}
   }
   await announce(s,key,r);
   needsCycle=!!await s.transaction(['revisions'],'readonly',tx=>request(tx.objectStore('revisions').index('metadataQueued').count(1)))||!!await s.state('work-reconcile')||result.error instanceof Error&&result.error.message==='CONTENT_REPLAN';
  }
 }finally{for(const x of running.values())x.controller.abort();await Promise.allSettled([...running.values()].map(x=>x.promise));}
}

export async function workActivity(s:Store,key:Uint8Array){
 const result=[];
 for(const task of await allTasks(s))if(task.error){
  const revision=await s.revision(task.revision),{metadata,key:fileKey}=unlockRevision(key,s.account.account,revision);if(fileKey)c.wipe(fileKey);
  result.push({revision:task.revision,name:metadata.name,direction:await s.contentAvailable(key,revision)?'upload' as const:'download' as const,phase:task.error==='source'?'waiting':'error',attempt:task.attempts,retryAt:task.retryAt,doneBytes:0,totalBytes:0,reusedBytes:0,chunk:0,chunks:0,updatedAt:Date.now()});
 }
 return result.slice(0,50);
}
