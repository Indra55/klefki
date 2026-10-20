import * as c from './crypto.ts';
import {syncSelective, type ContentSettings} from './selective-sync.ts';
import {syncQueue,queueNeeded} from './sync-workers.ts';
import {syncDeletions,cleanupTrash,deletionRecords,expiredTrash} from './trash.ts';
import { validateStatus, type SyncProgress, type Provenance, type ServerStatus, type DeviceState } from '../../ui/sync-status.ts';
import { Store, unlockRevision, type Revision,type DocumentEvent } from './store.ts';
export class HTTPError extends Error {
    constructor(readonly status: number, readonly after: number) { super('Klefki server returned ' + status); }
}
export class RelayWaiting extends Error { constructor(message='Waiting for another linked device to reconnect and supply documents.'){super(message);} }
export class MissingServerAccount extends Error { constructor(){super('This account is missing from the server. Local documents are still available. Reconnect a device with the saved relay policy, or register this store in Settings.');} }
export interface Session {
    token: string;
    device: string;
    expires: number;
}
async function wait(ms:number,signal:AbortSignal) {
    signal.throwIfAborted();
    await new Promise<void>((resolve,reject)=>{
        const abort=()=>{clearTimeout(timer);reject(signal.reason);};
        const timer=setTimeout(()=>{signal.removeEventListener('abort',abort);resolve();},ms);
        signal.addEventListener('abort',abort,{once:true});
    });
}
export async function chunkWindows<T>(count:number,signal:AbortSignal,fetch:(index:number,signal:AbortSignal)=>Promise<T>,consume:(index:number,value:T)=>void) {
    const controller=new AbortController(), combined=AbortSignal.any([signal,controller.signal]);
    try { for(let start=0;start<count;start+=4){
        combined.throwIfAborted();
        const tasks=Array.from({length:Math.min(4,count-start)},(_,j)=>fetch(start+j,combined).catch(e=>{controller.abort(e);throw e;}));
        const results=await Promise.allSettled(tasks);
        combined.throwIfAborted();
        results.forEach((result,j)=>{if(result.status==='rejected')throw result.reason;consume(start+j,result.value);});
    } } finally {controller.abort();}
}
class ContentBudget {
    active=0;
    private waiting:{resolve:()=>void;reject:(reason:unknown)=>void;signal:AbortSignal;abort:()=>void}[]=[];
    async acquire(signal:AbortSignal){
        signal.throwIfAborted();
        if(this.active<4){this.active++;return;}
        await new Promise<void>((resolve,reject)=>{const entry={resolve,reject,signal,abort:()=>{this.waiting=this.waiting.filter(x=>x!==entry);reject(signal.reason)}};this.waiting.push(entry);signal.addEventListener('abort',entry.abort,{once:true});});
    }
    release(){const next=this.waiting.shift();if(next){next.signal.removeEventListener('abort',next.abort);next.resolve();}else this.active--;}
}
export class Remote {
    private network={cooldown:0,budget:new ContentBudget()};
    singleAttempt=false;
    workQueue?:boolean;
    token = '';
    device = '';
    relayDevice = '';
    observe?: (progress: SyncProgress) => void;
    onContact?: () => void;
    onDocument?: (event:DocumentEvent)=>void;
    onSaved?:()=>void;
    progress: SyncProgress = {phase:'checking',doneBytes:0,totalBytes:0,reusedBytes:0,chunk:0,chunks:0,updatedAt:0};
    report(p: SyncProgress) { this.progress = {...p,updatedAt:Date.now()}; this.observe?.(this.progress); }
    phase(phase: string) { this.report({...this.progress,phase,attempt:undefined,retryAt:undefined}); }
    constructor(readonly origin: string, readonly signal: AbortSignal) {
        const u = new URL(origin);
        c.check(u.origin === origin && !u.username && !u.password && (u.protocol === 'https:' || (u.protocol === 'http:' && ['127.0.0.1', '[::1]', 'localhost'].includes(u.hostname))));
    }
    fork(signal:AbortSignal){const r=new Remote(this.origin,AbortSignal.any([this.signal,signal]));r.network=this.network;r.token=this.token;r.device=this.device;r.relayDevice=this.relayDevice;r.singleAttempt=true;r.observe=p=>this.report(p);r.onContact=this.onContact;r.onDocument=this.onDocument;r.onSaved=this.onSaved;return r;}
    async request(method: string, path: string, body?: unknown, limit = 24 * 1024 * 1024, options: {signal?:AbortSignal; origin?:string; onRead?:(size:number)=>void; background?:boolean} = {}): Promise<Uint8Array> {
        const signal=options.signal?AbortSignal.any([this.signal,options.signal]):this.signal;
        for (let attempt = 0;; attempt++) {
            while(this.network.cooldown>Date.now())await wait(this.network.cooldown-Date.now(),signal);
            signal.throwIfAborted();
            let delay = 250 * 2 ** attempt;
            let acquired=false;
            try {
                if(path.startsWith('/v1/blobs/')){await this.network.budget.acquire(signal);acquired=true;}signal.throwIfAborted();
                const controller = new AbortController(), timer = setTimeout(() => controller.abort(), options.background ? 3000 : 30000), abort = () => controller.abort();
                signal.addEventListener('abort', abort, { once: true });
                try {
                    const binary = body instanceof Uint8Array;
                    const res = await fetch(this.origin + path, { method, body: body === undefined ? undefined : binary ? new Blob([body as Uint8Array<ArrayBuffer>]) : JSON.stringify(body), headers: { 'X-Klefki-Deletion':'1', 'X-Klefki-Content':'1', ...(this.token ? { Authorization: 'Bearer ' + this.token } : {}), ...(this.relayDevice?{'X-Klefki-Device':this.relayDevice}:{}), ...(options.origin?{'X-Klefki-Origin-Device':options.origin}:{}), ...(body === undefined ? {} : { 'Content-Type': binary ? 'application/octet-stream' : 'application/json' }) }, redirect: 'error', credentials: 'omit', cache: 'no-store', signal: controller.signal });
                    if (!res.ok) {
                        await res.body?.cancel();
                        const seconds = Number(res.headers.get('Retry-After'));
                        throw new HTTPError(res.status, Number.isInteger(seconds) && seconds > 0 && seconds <= 120 ? seconds * 1000 : 0);
                    }
                    this.onContact?.();
                    const reader = res.body?.getReader();
                    if (!reader)
                        return new Uint8Array();
                    const parts: Uint8Array[] = [];
                    let size = 0,lastRead=0;
                    try {
                        for (;;) {
                            const { done, value } = await reader.read();
                            if (done)
                                break;
                            size += value.length;
                            c.check(size <= limit);
                            if(Date.now()-lastRead>=250){options.onRead?.(size);lastRead=Date.now();}
                            parts.push(value);
                        }
                        options.onRead?.(size);
                        return c.cat(...parts);
                    }
                    finally {
                        await reader.cancel();
                    }
                }
                finally {
                    clearTimeout(timer);
                    signal.removeEventListener('abort', abort);
                }
            }
            catch (e) {
                signal.throwIfAborted();
                const retry = e instanceof TypeError || (e instanceof DOMException && e.name === 'AbortError') || (e instanceof HTTPError && (e.status === 408 || e.status === 429 || e.status >= 500&&e.status!==507));
                if(e instanceof HTTPError&&e.after>0)this.network.cooldown=Math.max(this.network.cooldown,Date.now()+e.after);
                if (!retry || options.background || this.singleAttempt || attempt === 3)
                    throw e;
                if (e instanceof HTTPError)
                    delay = Math.max(delay, e.after);
                const previous = this.progress;
                this.report({...previous,phase:'retrying',attempt:attempt+2,retryAt:Date.now()+delay});
                await wait(delay,signal);
                this.report({...this.progress,phase:previous.phase,attempt:undefined,retryAt:undefined});
            }
            finally {if(acquired)this.network.budget.release();}
        }
    }
    async json<T>(method: string, path: string, body?: unknown, limit?: number): Promise<T> { return c.strictJSON(c.decode(await this.request(method, path, body, limit))) as T; }
    async login(a: c.Account, credential: Uint8Array, signup = false, restore = false) {
        let s:Session, restored=false;
        try { s=await this.json<Session>('POST', signup ? '/v1/signup' : '/v1/login', signup ? { ...a, credential: c.base64(credential) } : { account: a.account, credential: c.base64(credential) }, 8192); }
        catch(e){
            if (!(e instanceof HTTPError)||e.status!==401||signup) throw e;
            try { await this.request('GET','/v1/accounts/'+a.account,undefined,8192); throw e; }
            catch(lookup){ if (!(lookup instanceof HTTPError)||lookup.status!==404) throw e; }
            if(!restore)throw new MissingServerAccount();
            try { s=await this.json<Session>('POST','/v1/signup',{...a,credential:c.base64(credential)},8192);restored=true; }
            catch(signupError){ if (!(signupError instanceof HTTPError)||signupError.status!==409) throw signupError; s=await this.json<Session>('POST','/v1/login',{account:a.account,credential:c.base64(credential)},8192); }
        }
        c.check(/^[0-9a-f]{64}$/.test(s.token) && Number.isSafeInteger(s.expires) && s.expires > 0);
        c.validID(s.device);
        this.token = s.token;
        this.device = s.device;
        if(restored) await this.request('PUT','/v1/sync/settings',{mode:'temporary-relay'},1024);
    }
}

export async function setSyncMode(store:Store,master:Uint8Array,remote:Remote,mode:string){
    c.check(mode==='persistent'||mode==='temporary-relay');
    await remote.request('PUT','/v1/sync/settings',{mode},1024);
    await store.setState('sync-mode',mode);
    await sync(store,master,remote);
}

export const startProgress = (phase:string,r:Revision,name:string,direction:'upload'|'download',totalBytes:number,chunks:number,source='',publisher=''): SyncProgress => ({phase,revision:r.id,name,direction,totalBytes,chunks,source,publisher,doneBytes:0,reusedBytes:0,chunk:0,updatedAt:Date.now()});

export async function needsSync(store: Store, remote: Remote) {
    if(await store.state('work-active'))return queueNeeded(store,remote);
    if ((await store.all()).some(x => x.pending)||(await deletionRecords(store,true)).length) return true;
    const settings = await remote.json<{mode:string;epoch:string;queued:number;deletion_sequence?:number;trash_policy_version?:number;content_version?:number}>('GET','/v1/sync/settings',undefined,1024);
    if((settings.trash_policy_version||0)!==(await store.state<number>('trash-policy-version')||0)||(settings.content_version||0)!==(await store.state<number>('content-version')||0))return true;
    if((settings.deletion_sequence||0)!==(await store.state<number>('deletion-cursor')||0))return true;
    const local = await store.relayState(), cursor = await store.state<number>('cursor') || 0;
    if (settings.epoch !== await store.state('relay-epoch') || settings.mode !== await store.state('sync-mode')) return true;
    const status = validateStatus(await remote.json<ServerStatus>('GET','/v1/sync/status',undefined,32768));
    return status.epoch !== settings.epoch || status.latestSequence !== cursor || status.devices.length !== local.known.length ||
        status.devices.some(peer => !local.known.includes(peer.device) || (peer.device === local.device && peer.sequence !== cursor) || (settings.mode === 'temporary-relay' && settings.queued === 0 && peer.sequence < cursor));
}

export async function provenance(remote: Remote,after:number) {
    const result = new Map<string,Provenance>();
    try {
        const b = await remote.request('GET','/v1/sync/provenance?after='+after,undefined,8192,{background:true});
        const page = c.strictJSON(c.decode(b)) as {changes:Provenance[]};
        c.check(Array.isArray(page.changes)&&page.changes.length<=16);
        for (const p of page.changes) { c.validID(p.revision);if(p.origin)c.validID(p.origin);if(p.publisher)c.validID(p.publisher);c.check(Number.isSafeInteger(p.sequence)&&p.sequence>after);result.set(p.revision,p); }
    } catch { result.clear(); }
    return result;
}

export async function push(store:Store,master:Uint8Array,remote:Remote,r:Revision,guard?:()=>Promise<void>){
        remote.signal.throwIfAborted();
        const { metadata: m, key } = unlockRevision(master, store.account.account, r);
        remote.report(startProgress('verifying',r,m.name,'upload',m.size||0,m.chunk_count||0));
        const origin = await store.origin(r.id);
        if (key)
            try {
                const chunks = await store.chunks(m.file_id!, m.chunk_count!), plain = c.decryptFile(key, store.account.account, m.file_id!, m.size!, chunks);
                c.wipe(plain);
                remote.phase('uploading');
                const present = new Set<number>();
                let after = -1;
                for (;;) {
                    const page = await remote.json<number[]>('GET', `/v1/blobs/${m.file_id}/chunks${after >= 0 ? '?after=' + after : ''}`, undefined, 32768);
                    c.check(Array.isArray(page) && page.length <= 1000);
                    if (!page.length)
                        break;
                    for (const index of page) {
                        c.check(Number.isSafeInteger(index) && index > after && index < m.chunk_count!);
                        present.add(index);
                        after = index;
                    }
                }
                await chunkWindows(chunks.length,remote.signal,async(i,signal)=>{
                    await guard?.();
                    if(!present.has(i))await remote.request('PUT',`/v1/blobs/${m.file_id}/chunks/${i}`,chunks[i],1024,{signal});
                    return present.has(i);
                },(i,reused)=>remote.report({...remote.progress,chunk:i+1,doneBytes:Math.min(m.size!,(i+1)*c.CHUNK),reusedBytes:remote.progress.reusedBytes+(reused?Math.min(c.CHUNK,m.size!-i*c.CHUNK):0)}));
            }
            finally {
                c.wipe(key);
            }
        remote.phase('committing');
        const result = c.strictJSON(c.decode(await remote.request('PUT','/v1/sync/revisions/'+r.id,r,1024,{origin}))) as {
            sequence: number;
        };
        c.check(Number.isSafeInteger(result.sequence) && result.sequence > 0);
        await store.acknowledge(r.id);
        remote.phase('complete');
}

export async function sync(store:Store,master:Uint8Array,remote:Remote){
 try{await syncPass(store,master,remote);}catch(e){if(!(e instanceof HTTPError)||e.status!==410)throw e;await syncDeletions(store,master,remote);await syncPass(store,master,remote);}
}
async function syncPass(store:Store,master:Uint8Array,remote:Remote){
 remote.relayDevice=(await store.relayState()).device;
 for(;;){const settings=await remote.json<ContentSettings>('GET','/v1/sync/settings',undefined,1024);
 if(!settings.selective_content)throw Error('UPDATE_REQUIRED: Update the server for selective content sync.');remote.workQueue=!!settings.work_queue;if(!settings.work_queue){await store.setState('work-active',false);await store.setState('work-reconcile',true);}
 try{if(settings.work_queue)await syncQueue(store,master,remote,settings);else await syncSelective(store,master,remote,settings);break;}catch(e){if(!(e instanceof Error)||e.message!=='CONTENT_REPLAN')throw e;}}
 await syncDeletions(store,master,remote);
 await cleanupTrash(store,master,remote);
}
export async function reportDeviceState(store:Store,remote:Remote,status:ServerStatus,refresh=true) {
    if(!status.deviceStateSupported)return;
    const local=await store.relayState(),peer=status.devices.find(d=>d.device===local.device);
    if(!peer)return;
    if(!refresh) {
        const saved=await store.state<{version:number;pending:boolean}>('device-state');
        if(saved&&peer.deviceState?.version===saved.version&&peer.deviceState.pending===saved.pending)return;
    }
    let report=await store.deviceState();
    const previous=peer.deviceState;
    if(previous?.version===report.version&&previous.pending===report.pending)return;
    if(previous&&previous.version>=report.version)report=await store.deviceState(previous.version+1);
    const result=c.strictJSON(c.decode(await remote.request('POST','/v1/sync/device-state',{device:local.device,...report},1024,{background:true}))) as DeviceState;
    c.check(result.version===report.version&&result.pending===report.pending&&Number.isSafeInteger(result.reportedAt)&&result.reportedAt>=0);
    return result;
}
