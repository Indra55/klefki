import * as c from './crypto.ts';
import { type SyncProgress, type Provenance } from '../../ui/sync-status.ts';
import { Store, unlockRevision, type Revision } from './store.ts';
export class HTTPError extends Error {
    constructor(readonly status: number, readonly after: number) { super('Klefki server returned ' + status); }
}
export class RelayWaiting extends Error { constructor(){ super('Waiting for another linked device to reconnect and supply documents.'); } }
export interface Session {
    token: string;
    device: string;
    expires: number;
}
export class Remote {
    token = '';
    device = '';
    relayDevice = '';
    observe?: (progress: SyncProgress) => void;
    onContact?: () => void;
    progress: SyncProgress = {phase:'checking',doneBytes:0,totalBytes:0,reusedBytes:0,chunk:0,chunks:0,updatedAt:0};
    report(p: SyncProgress) { this.progress = {...p,updatedAt:Date.now()}; this.observe?.(this.progress); }
    phase(phase: string) { this.report({...this.progress,phase,attempt:undefined,retryAt:undefined}); }
    constructor(readonly origin: string, readonly signal: AbortSignal) {
        const u = new URL(origin);
        c.check(u.origin === origin && !u.username && !u.password && (u.protocol === 'https:' || (u.protocol === 'http:' && ['127.0.0.1', '[::1]', 'localhost'].includes(u.hostname))));
    }
    async request(method: string, path: string, body?: unknown, limit = 24 * 1024 * 1024, options: {origin?:string; onRead?:(size:number)=>void; background?:boolean} = {}): Promise<Uint8Array> {
        for (let attempt = 0;; attempt++) {
            this.signal.throwIfAborted();
            let delay = 250 * 2 ** attempt;
            try {
                const controller = new AbortController(), timer = setTimeout(() => controller.abort(), options.background ? 3000 : 30000), abort = () => controller.abort();
                this.signal.addEventListener('abort', abort, { once: true });
                try {
                    const binary = body instanceof Uint8Array;
                    const res = await fetch(this.origin + path, { method, body: body === undefined ? undefined : binary ? new Blob([body as Uint8Array<ArrayBuffer>]) : JSON.stringify(body), headers: { ...(this.token ? { Authorization: 'Bearer ' + this.token } : {}), ...(this.relayDevice?{'X-Klefki-Device':this.relayDevice}:{}), ...(options.origin?{'X-Klefki-Origin-Device':options.origin}:{}), ...(body === undefined ? {} : { 'Content-Type': binary ? 'application/octet-stream' : 'application/json' }) }, redirect: 'error', credentials: 'omit', cache: 'no-store', signal: controller.signal });
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
                    this.signal.removeEventListener('abort', abort);
                }
            }
            catch (e) {
                this.signal.throwIfAborted();
                const retry = e instanceof TypeError || (e instanceof DOMException && e.name === 'AbortError') || (e instanceof HTTPError && (e.status === 408 || e.status === 429 || e.status >= 500&&e.status!==507));
                if (!retry || options.background || attempt === 3)
                    throw e;
                if (e instanceof HTTPError)
                    delay = Math.max(delay, e.after);
                const previous = this.progress;
                this.report({...previous,phase:'retrying',attempt:attempt+2,retryAt:Date.now()+delay});
                await new Promise<void>((resolve, reject) => { const abort = () => { clearTimeout(timer); reject(Error('Locked')); }; const timer = setTimeout(() => { this.signal.removeEventListener('abort', abort); resolve(); }, delay); this.signal.addEventListener('abort', abort, { once: true }); });
                this.report(previous);
            }
        }
    }
    async json<T>(method: string, path: string, body?: unknown, limit?: number): Promise<T> { return c.strictJSON(c.decode(await this.request(method, path, body, limit))) as T; }
    async login(a: c.Account, credential: Uint8Array, signup = false, restore = false) {
        let s:Session, restored=false;
        try { s=await this.json<Session>('POST', signup ? '/v1/signup' : '/v1/login', signup ? { ...a, credential: c.base64(credential) } : { account: a.account, credential: c.base64(credential) }, 8192); }
        catch(e){
            if (!(e instanceof HTTPError)||e.status!==401||!restore) throw e;
            try { await this.request('GET','/v1/accounts/'+a.account,undefined,8192); throw e; }
            catch(lookup){ if (!(lookup instanceof HTTPError)||lookup.status!==404) throw e; }
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
    await sync(store,master,remote);
}

const startProgress = (phase:string,r:Revision,name:string,direction:'upload'|'download',totalBytes:number,chunks:number,source='',publisher=''): SyncProgress => ({phase,revision:r.id,name,direction,totalBytes,chunks,source,publisher,doneBytes:0,reusedBytes:0,chunk:0,updatedAt:Date.now()});

async function provenance(remote: Remote,after:number) {
    const result = new Map<string,Provenance>();
    try {
        const b = await remote.request('GET','/v1/sync/provenance?after='+after,undefined,8192,{background:true});
        const page = c.strictJSON(c.decode(b)) as {changes:Provenance[]};
        c.check(Array.isArray(page.changes)&&page.changes.length<=16);
        for (const p of page.changes) { c.validID(p.revision);if(p.origin)c.validID(p.origin);if(p.publisher)c.validID(p.publisher);c.check(Number.isSafeInteger(p.sequence)&&p.sequence>after);result.set(p.revision,p); }
    } catch { result.clear(); }
    return result;
}

async function push(store:Store,master:Uint8Array,remote:Remote,r:Revision){
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
                for (let i = 0; i < chunks.length; i++) {
                    remote.report({...remote.progress,chunk:i+1});
                    if (!present.has(i)) await remote.request('PUT', `/v1/blobs/${m.file_id}/chunks/${i}`, chunks[i], 1024);
                    remote.report({...remote.progress,doneBytes:Math.min(m.size!, (i+1)*c.CHUNK),reusedBytes:remote.progress.reusedBytes+(present.has(i)?Math.min(c.CHUNK,m.size!-i*c.CHUNK):0)});
                }
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

export async function sync(store: Store, master: Uint8Array, remote: Remote) {
    remote.report({...remote.progress,phase:'checking',revision:undefined,name:undefined,direction:undefined,doneBytes:0,totalBytes:0,reusedBytes:0,chunk:0,chunks:0});
    await store.bind(remote.origin);
    let settings:{mode:string;epoch:string}|undefined;
    let quota:HTTPError|undefined;
    try { settings=await remote.json('GET','/v1/sync/settings',undefined,1024); }
    catch(e){ if(!(e instanceof HTTPError)||e.status!==404||await store.state('sync-mode')==='temporary-relay') throw e; }
    if(settings){
        c.check(settings.mode==='persistent'||settings.mode==='temporary-relay');c.validID(settings.epoch);
        const local=await store.relayState();
        const state=await remote.json<{mode:string;epoch:string;devices:string[]}>('POST','/v1/sync/relay/device',{...local,sequence:await store.state<number>('cursor')||0},8192);
        await store.saveRelay(state.mode,state.epoch,state.devices);
        remote.relayDevice=local.device;
        const all=(await store.all()).sort((a,b)=>a.order-b.order);
        try { for(let start=0;start<all.length;start+=16){
            const batch=all.slice(start,start+16),ids=batch.map(x=>x.revision.id);
            const needed=await remote.json<{ids:string[]}>('POST','/v1/sync/relay/need',{ids},2048);
            c.check(Array.isArray(needed.ids)&&needed.ids.length<=ids.length&&needed.ids.every(id=>ids.includes(id)));
            for(const {revision:r} of batch) if(needed.ids.includes(r.id)) await push(store,master,remote,r);
        } } catch(e){if(!(e instanceof HTTPError)||e.status!==507)throw e;quota=e;}
    }
    if(!quota)for (const { revision: r } of (await store.all()).filter(x => x.pending).sort((a, b) => a.order - b.order)) await push(store,master,remote,r);
    let cursor = await store.state<number>('cursor') || 0;
    for (;;) {
        const page = await remote.json<{
            changes: {
                sequence: number;
                revision: Revision;
            }[];
        }>('GET', '/v1/sync/changes?after=' + cursor);
        c.check(Array.isArray(page.changes) && page.changes.length <= 16);
        if (!page.changes.length)
            break;
        const sources = page.changes.length ? await provenance(remote,cursor) : new Map<string,Provenance>();
        for (const change of page.changes) {
            c.check(Number.isSafeInteger(change.sequence) && change.sequence > cursor);
            let r = change.revision;
            if(r.metadata===''){
                const existing=(await store.all()).find(x=>x.revision.id===r.id)?.revision;
                if(!existing){await remote.request('PUT','/v1/sync/cursor',{sequence:cursor},1024);throw new RelayWaiting();}
                c.check(existing.object===r.object&&existing.base===r.base);r=existing;
            }
            const { metadata: m, key } = unlockRevision(master, store.account.account, r), chunks: Uint8Array[] = [];
            const source = sources.get(r.id);
            remote.report(startProgress('downloading',r,m.name,'download',m.size||0,m.chunk_count||0,source?.origin,source?.publisher));
            if (key)
                try {
                    // Validate each bounded chunk before accumulating ciphertext. No partial import is visible.
                    let total = 0;
                    let localChunks:Uint8Array[]|undefined;
                    try { localChunks=await store.chunks(m.file_id!,m.chunk_count!); } catch { }
                    for (let i = 0; i < m.chunk_count!; i++) {
                        remote.report({...remote.progress,chunk:i+1});
                        const base = i*c.CHUNK;
                        const b = localChunks?.[i] || await remote.request('GET', `/v1/blobs/${m.file_id}/chunks/${i}`, undefined, c.CHUNK + 87,{onRead:n=>{if(n>87)remote.report({...remote.progress,doneBytes:base+Math.min(n-87,m.size!-base)});}});
                        const p = c.open(key, { kind: 5, account: store.account.account, object: m.file_id!, index: i, final: i === m.chunk_count! - 1 }, b);
                        total += p.length;
                        c.wipe(p);
                        chunks.push(b);
                        remote.report({...remote.progress,doneBytes:total,reusedBytes:remote.progress.reusedBytes+(localChunks?.[i]?b.length-87:0)});
                    }
                    c.check(total === m.size);
                }
                finally {
                    c.wipe(key);
                }
            remote.signal.throwIfAborted();
            remote.phase('committing');
            await store.commit(r, chunks, m.file_id, false, change.sequence,source?.origin);
            remote.phase('complete');
            cursor = change.sequence;
        }
    }
    remote.report({...remote.progress,phase:'acknowledging',revision:undefined,name:undefined,direction:undefined,totalBytes:0,doneBytes:0,reusedBytes:0,chunk:0,chunks:0});
    await remote.request('PUT', '/v1/sync/cursor', { sequence: cursor }, 1024);
    if(quota)throw quota;
}
