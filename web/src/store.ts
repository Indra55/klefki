import * as c from './crypto.ts';
import {transferTree,type TransferItem} from '../../ui/library-transfer.ts';
export interface Revision {
    id: string;
    object: string;
    base: string;
    metadata: string;
    file_key?: string;
}
export interface Stored {
    revision: Revision;
    pending: boolean;
    metadataPending?: boolean;
    uploadPending?: boolean;
    order: number;
    origin?: string;
    deferred?: boolean;
    wanted?: boolean;
    explicit?: boolean;
    trash?: boolean;
    sequence?: number;
    file?: string;
    metadataQueued?: number;
    metadataRetry?: WorkRetry;
}
export type WorkRetry={attempts:number;retryAt:number;status?:number};
export type WorkTask=WorkRetry&{revision:string;file:string;direction:'plan'|'upload'|'download'|'receipt';priority:number;error:string;uploadPending?:boolean};
export type DocumentEvent={revision:string;name:string;source:string;stage:'receiving'|'ready';created:number;read:boolean};
export const fileIdentity=(r:Revision)=>{if(!r.file_key)return '';const key=c.unbase64(r.file_key);c.check(key.length===106);return c.hex(key.subarray(18,34));};
export const workTask=(r:Revision,uploadPending=false):WorkTask=>({revision:r.id,file:fileIdentity(r),direction:'plan',priority:0,attempts:0,retryAt:0,error:'',uploadPending});
export interface Head {
    revision: Revision;
    metadata: c.Metadata;
    conflict: boolean;
    contentMissing?: boolean;
}
export function validateRevision(r: Revision) { c.validID(r.id); c.validID(r.object); if (r.base !== '')
    c.validID(r.base); c.check(r.base !== r.id && typeof r.metadata === 'string' && (r.file_key === undefined || typeof r.file_key === 'string')); }
function sameRevision(a: Revision, b: Revision) { return a.id === b.id && a.object === b.object && a.base === b.base && a.metadata === b.metadata && (a.file_key || '') === (b.file_key || ''); }
export function unlockRevision(master: Uint8Array, account: string, r: Revision) {
    validateRevision(r);
    const metadata = c.decryptMetadata(master, account, r.object, c.unbase64(r.metadata));
    let key: Uint8Array | undefined;
    if (metadata.type === 'file') {
        c.check(r.file_key);
        key = c.open(master, { kind: 3, account, object: metadata.file_id! }, c.unbase64(r.file_key));
    }
    else
        c.check(!r.file_key);
    return { metadata, key };
}
const req = <T>(r: IDBRequest<T>) => new Promise<T>((resolve, reject) => { r.onsuccess = () => resolve(r.result); r.onerror = () => reject(r.error || Error('Local klefki storage failed')); });
const done = (tx: IDBTransaction) => new Promise<void>((resolve, reject) => { tx.oncomplete = () => resolve(); tx.onabort = tx.onerror = () => reject(tx.error || Error('Local klefki transaction failed')); });
export const storeReference = (account: string) => 'klefki-store-' + account;
export class Store {
    onChange?: () => void;
    onMutation?: () => void;
    created = false;
    missing = false;
    private constructor(readonly db: IDBDatabase, readonly account: c.Account) { }
    static async open(account: c.Account, stagingName?: string) {
        c.validateAccount(account);
        const base = 'klefki-v1-' + account.account;
        const saved = localStorage.getItem(storeReference(account.account));
        const name = stagingName || saved || base;
        c.check(name === base || new RegExp('^' + base + '-restore-[0-9a-f]{32}$').test(name));
        let created = false;
        const r = indexedDB.open(name, 3);
        r.onupgradeneeded = event => { created = event.oldVersion === 0; const db = r.result; if(!db.objectStoreNames.contains('state')){db.createObjectStore('state'); db.createObjectStore('revisions', { keyPath: 'revision.id' }); db.createObjectStore('chunks');} if(!db.objectStoreNames.contains('deletions'))db.createObjectStore('deletions',{keyPath:'object'});
            const records=r.transaction!.objectStore('revisions');for(const [name,path] of [['object','revision.object'],['file','file'],['metadataQueued','metadataQueued'],['sequence','sequence']] as const)if(!records.indexNames.contains(name))records.createIndex(name,path);
            if(!db.objectStoreNames.contains('work')){const work=db.createObjectStore('work',{keyPath:'revision'});work.createIndex('file','file');work.createIndex('retryAt','retryAt');}
            if(!db.objectStoreNames.contains('incoming'))db.createObjectStore('incoming',{keyPath:'revision'});
        };
        const db = await req(r);
        db.onversionchange = () => db.close();
        const store = new Store(db, account);
        store.created = created;
        store.missing = !stagingName && created && (!!saved || localStorage.getItem('klefki-account') === JSON.stringify(account));
        try {
        await store.transaction(['state'], 'readwrite', async (tx) => { const state = tx.objectStore('state'), old = await req(state.get('account')); if (old)
            c.check(old.account === account.account && old.descriptor === account.descriptor && old.password_wrapper === account.password_wrapper && old.recovery_wrapper === account.recovery_wrapper);
        else
            state.put(account, 'account');
            if(await req(state.get('sync-connected'))===undefined)state.put(!!await req(state.get('origin')),'sync-connected');
            const device = await req(state.get('relay-device')); if (!device) state.put(c.id(),'relay-device');
            if (store.missing) { state.put(true, 'local-recovery'); state.put(true, 'sync-paused'); }
        });
        if (!stagingName) localStorage.setItem(storeReference(account.account), name);
        return store;
        } catch(error) {db.close();throw error;}
    }
    close() { this.db.close(); }
    async transaction<T>(names: string[], mode: IDBTransactionMode, fn: (tx: IDBTransaction) => Promise<T>, notify = true): Promise<T> {
        const tx = this.db.transaction(names, mode), completed = done(tx);
        try {
            const result = await fn(tx);
            await completed;
            if (mode === 'readwrite' && notify) this.onMutation?.();
            return result;
        }
        catch (e) {
            try {
                tx.abort();
            }
            catch { }
            await completed.catch(() => { });
            throw e;
        }
    }
    async state<T>(key: string): Promise<T | undefined> { return this.transaction(['state'], 'readonly', tx => req(tx.objectStore('state').get(key))); }
    async setState(key:string,value:unknown){await this.transaction(['state'],'readwrite',async tx=>{tx.objectStore('state').put(value,key);});}
    async contentAvailable(master:Uint8Array,r:Revision){
        const {metadata:m,key}=unlockRevision(master,this.account.account,r);if(key)c.wipe(key);
        if(m.type==='folder')return true;
        if(await this.state('invalid-content:'+m.file_id))return false;
        return await this.transaction(['chunks'],'readonly',tx=>req(tx.objectStore('chunks').count(IDBKeyRange.bound(m.file_id!+':',m.file_id!+':\uffff'))))===m.chunk_count;
    }
    async chunk(file:string,index:number):Promise<Uint8Array|undefined>{return this.transaction(['chunks'],'readonly',tx=>req(tx.objectStore('chunks').get(file+':'+index)));}
    async offlineHeads(master:Uint8Array){
        const heads=await this.heads(master,true);
        for(const head of heads)head.contentMissing=!(await this.contentAvailable(master,head.revision));
        return heads;
    }
    async contentFlags(id:string,change:Partial<Pick<Stored,'pending'|'deferred'|'wanted'|'explicit'|'trash'|'uploadPending'>>){
        await this.transaction(['revisions','work'],'readwrite',async tx=>{const store=tx.objectStore('revisions'),record=await req(store.get(id)) as Stored|undefined;c.check(record);store.put({...record,...change,pending:!!record.metadataPending||!!change.pending});if(change.explicit&&record.revision.file_key)tx.objectStore('work').put(workTask(record.revision));});
    }
    async deviceState(minimum=0) {
        return this.transaction(['state','revisions','deletions','work'],'readwrite',async tx => {
            const state=tx.objectStore('state');
            const revisions=await req(tx.objectStore('revisions').getAll()) as Stored[];
            const deletions=await req(tx.objectStore('deletions').getAll()) as {pending:boolean}[];
            const queued=await req(state.get('work-active'));
            const pending=(queued?revisions.some(x=>x.metadataQueued||x.metadataPending)||(await req(tx.objectStore('work').getAll()) as WorkTask[]).some(x=>x.uploadPending):revisions.some(x=>x.pending))||deletions.some(x=>x.pending);
            const previous=await req(state.get('device-state')) as {version:number;pending:boolean}|undefined;
            const version=!previous||previous.pending!==pending||previous.version<minimum?Math.max((previous?.version||0)+1,minimum):previous.version;
            c.check(Number.isSafeInteger(version)&&version>0);
            const report={version,pending};
            if(!previous||previous.version!==version)state.put(report,'device-state');
            return report;
        });
    }
    async relayState() {
        return this.transaction(['state'],'readwrite',async tx => {
            const state=tx.objectStore('state'), device=await req(state.get('relay-device')) || c.id();
            state.put(device,'relay-device');
            return {device:device as string,known:(await req(state.get('relay-devices')) || []) as string[]};
        });
    }
    async saveRelay(mode: string, epoch: string, devices: string[]) {
        c.check(mode==='persistent'||mode==='temporary-relay'); c.validID(epoch);
        c.check(Array.isArray(devices)&&devices.length<=64); devices.forEach(c.validID);
        await this.transaction(['state'],'readwrite',async tx => {
            const state=tx.objectStore('state');
            c.check(devices.includes(await req(state.get('relay-device'))));
            if(await req(state.get('sync-mode'))!==mode){state.put(-1,'work-inventory');state.put(true,'work-reconcile');}
            if (await req(state.get('relay-epoch'))!==epoch) {state.put(0,'cursor');state.put(0,'metadata-cursor');state.put(0,'trash-policy-version');state.put(0,'content-version');state.put(0,'work-content-cursor');state.put(-1,'work-inventory');state.put(true,'work-reconcile');}
            state.put(epoch,'relay-epoch');state.put(mode,'sync-mode');state.put(devices,'relay-devices');
        });
    }
    async bind(origin: string) { await this.transaction(['state'], 'readwrite', async (tx) => { const s = tx.objectStore('state'), old = await req(s.get('origin')); c.check(old === undefined || old === origin); s.put(origin, 'origin'); }); }
    async all(): Promise<Stored[]> { return this.transaction(['revisions'], 'readonly', tx => req(tx.objectStore('revisions').getAll())); }
    async history(): Promise<{id:string;order:number}[]> {
        return this.transaction(['revisions'],'readonly',tx => new Promise((resolve,reject) => {
            const result:{id:string;order:number}[]=[];
            const cursor=tx.objectStore('revisions').openCursor();
            cursor.onerror=()=>reject(Error('Local klefki storage failed'));
            cursor.onsuccess=()=>{ const row=cursor.result;if(!row){resolve(result.sort((a,b)=>a.order-b.order));return;}
                const value=row.value as Stored;result.push({id:value.revision.id,order:value.order});row.continue(); };
        }));
    }
    async existing(r: Revision): Promise<Revision | undefined> {
        const x: Stored | undefined = await this.transaction(['revisions'], 'readonly', tx => req(tx.objectStore('revisions').get(r.id)));
        if (!x) return;
        c.check(x.revision.object === r.object && x.revision.base === r.base);
        if (r.metadata !== '') c.check(sameRevision(x.revision, r));
        return x.revision;
    }
    async origin(id: string): Promise<string> { const x: Stored | undefined = await this.transaction(['revisions'], 'readonly', tx => req(tx.objectStore('revisions').get(id))); return x?.origin || ''; }
    async revision(id: string): Promise<Revision> { c.validID(id); const x = await this.transaction(['revisions'], 'readonly', tx => req(tx.objectStore('revisions').get(id))); c.check(x); return x.revision; }
    async heads(master: Uint8Array, includeDeferred=false): Promise<Head[]> { const all = await this.all(), parents = new Set(all.map(x => x.revision.base)), heads = all.filter(x => !parents.has(x.revision.id)&&(includeDeferred||!x.deferred||x.trash===true)); return heads.map(x => { const { metadata, key } = unlockRevision(master, this.account.account, x.revision); if (key)
        c.wipe(key); return { revision: x.revision, metadata, contentMissing:!!x.deferred, conflict: heads.filter(y => y.revision.object === x.revision.object).length > 1 }; }); }
    async chunks(file: string, count: number) { c.validID(file); return this.transaction(['chunks'], 'readonly', async (tx) => { const s = tx.objectStore('chunks'), out: Uint8Array[] = []; for (let i = 0; i < count; i++) {
        const b = await req(s.get(file + ':' + i));
        c.check(b instanceof Uint8Array);
        out.push(b);
    } return out; }); }
    async export(master: Uint8Array, r: Revision) { const { metadata: m, key } = unlockRevision(master, this.account.account, r); c.check(key); try {
        try{return c.decryptFile(key, this.account.account, m.file_id!, m.size!, await this.chunks(m.file_id!,m.chunk_count!));}
        catch(error){
            await this.transaction(['state','revisions','work'],'readwrite',async tx=>{
                tx.objectStore('state').put(true,'invalid-content:'+m.file_id);
                for(const record of await req(tx.objectStore('revisions').index('file').getAll(m.file_id!)) as Stored[])tx.objectStore('work').put(workTask(record.revision));
            });
            throw error;
        }
    }
    finally {
        c.wipe(key);
    } }
    async commit(r: Revision, chunks: Uint8Array[], file: string | undefined, pending: boolean, sequence?: number, origin?: string, deferred?: boolean, metadataOnly=false) {
        validateRevision(r);
        await this.transaction(['revisions', 'chunks', 'state','deletions','work','incoming'], 'readwrite', async (tx) => {
            const revisions = tx.objectStore('revisions'), state = tx.objectStore('state'), blobs = tx.objectStore('chunks');
            if(await req(tx.objectStore('deletions').get(r.object))){c.check(!pending);if(sequence!==undefined)state.put(sequence,metadataOnly?'metadata-cursor':'cursor');return;}
            const old: Stored | undefined = await req(revisions.get(r.id));
            if (old){
                c.check(sameRevision(old.revision, r));
                c.check(!(metadataOnly&&deferred&&!old.deferred));
            }
            if (r.base) {
                const base: Stored | undefined = await req(revisions.get(r.base));
                c.check(base?.revision.object === r.object);
            }
            if (pending && r.base) {
                const all: Stored[] = await req(revisions.getAll());
                c.check(!all.some(x => x.revision.base === r.base));
            }
            for (let i = 0; i < chunks.length; i++) {
                c.check(file);
                const name = file + ':' + i, existing = await req(blobs.get(name));
                if(existing && await req(state.get('invalid-content:'+file)) && deferred===false && !pending)
                    blobs.put(chunks[i],name);
                else if (existing)
                    c.check(existing instanceof Uint8Array && existing.length === chunks[i].length && existing.every((value, index) => value === chunks[i][index]));
                else
                    blobs.add(chunks[i], name);
            }
            if(file&&chunks.length&&deferred===false)state.delete('invalid-content:'+file);
            if (!old) {
                const order = (await req(state.get('order')) || 0) + 1;
                state.put(order, 'order');
                const source = pending ? await req(state.get('relay-device')) as string : origin;
                if(metadataOnly&&r.file_key&&await req(state.get('work-baseline'))&&source!==await req(state.get('relay-device'))&&!(await req(revisions.index('object').count(r.object))))tx.objectStore('incoming').add({revision:r.id,source:source||'',created:Date.now(),read:false,reported:false});
                const fileID=fileIdentity(r);
                revisions.add({ revision: r, pending, metadataPending:pending,metadataQueued:pending?1:0,file:fileID||undefined, order, ...(source ? {origin:source} : {}),...(deferred?{deferred:true}:{}),...(sequence!==undefined?{sequence}:{}) } satisfies Stored);
                if(fileID)tx.objectStore('work').put(workTask(r,pending));
                if(!r.file_key&&r.base)state.put(true,'work-reconcile');
            }
            if(old && deferred!==undefined)revisions.put({...old,deferred,pending:pending||!!old.uploadPending||!!old.metadataPending||(deferred!==false&&!!old.deferred&&!!old.wanted),...(deferred===false?{wanted:false,explicit:false}:{}),...(sequence!==undefined?{sequence}:{})});
            if(deferred===false && file){
                for(const record of await req(revisions.getAll()) as Stored[]){
                    if(record.deferred&&record.revision.file_key===r.file_key)revisions.put({...record,deferred:false,pending:!!record.metadataPending,wanted:false,explicit:false});
                }
            }
            if (sequence !== undefined) {
                const cursorKey=metadataOnly?'metadata-cursor':'cursor';
                const cursor = await req(state.get(cursorKey)) || 0;
                c.check(Number.isSafeInteger(sequence) && sequence > cursor);
                state.put(sequence, cursorKey);
            }
        });
        if (pending) this.onChange?.();
    }
    async acknowledge(id: string) { await this.transaction(['revisions'], 'readwrite', async (tx) => { const s = tx.objectStore('revisions'), x: Stored = await req(s.get(id)); c.check(x); x.pending = !!x.uploadPending||!!x.deferred&&!!x.wanted;x.metadataPending=false;x.metadataQueued=0;delete x.metadataRetry; s.put(x); }); }
    private preparePut(master: Uint8Array, m: c.Metadata, bytes?: Uint8Array, base?: Revision) {
        const object = base?.object || c.id(), r: Revision = { id: c.id(), object, base: base?.id || '', metadata: '' }, chunks: Uint8Array[] = [];
        let file: string | undefined;
        if (m.type === 'file') {
            c.check(bytes);
            file = c.id();
            m = { ...m, file_id: file, size: bytes.length, chunk_count: c.chunkCount(bytes.length) };
            const key = c.random(32);
            try {
                r.file_key = c.base64(c.seal(master, { kind: 3, account: this.account.account, object: file }, key));
                for (let i = 0; i < m.chunk_count!; i++)
                    chunks.push(c.seal(key, { kind: 5, account: this.account.account, object: file, index: i, final: i === m.chunk_count! - 1 }, bytes.subarray(i * c.CHUNK, (i + 1) * c.CHUNK)));
            }
            finally {
                c.wipe(key);
            }
        }
        r.metadata = c.base64(c.encryptMetadata(master, this.account.account, object, m));
        return {r,chunks,file};
    }
    async put(master: Uint8Array, m: c.Metadata, bytes?: Uint8Array, base?: Revision) {
        if(m.parent)c.check(!await this.transaction(['deletions'],'readonly',tx=>req(tx.objectStore('deletions').get(m.parent!))));
        const {r,chunks,file}=this.preparePut(master,m,bytes,base);
        await this.commit(r,chunks,file,true);
        return r;
    }
    async transfer(master:Uint8Array, heads:Head[], items:TransferItem[], revisions:string[], destination:string|null, copy:boolean, active:()=>boolean) {
        const plan=transferTree(items,revisions,destination,copy), objects=new Map<string,string>();
        const prepared:ReturnType<Store['preparePut']>[]=[];
        for(let n=0;n<plan.length;n++){
            c.check(active());
            const item=plan[n],h=heads.find(h=>h.revision.id===item.revision)!;
            const parent=n<revisions.length?destination:objects.get(item.parent!)!;
            if(copy){
                let bytes:Uint8Array|undefined;
                try{
                    if(h.metadata.type==='file')bytes=await this.export(master,h.revision);
                    c.check(active());
                    const entry=this.preparePut(master,{...h.metadata,parent},bytes);
                    prepared.push(entry);objects.set(item.object,entry.r.object);
                }finally{if(bytes)c.wipe(bytes);}
            }else{
                prepared.push({r:{...h.revision,id:c.id(),base:h.revision.id,metadata:c.base64(c.encryptMetadata(master,this.account.account,item.object,{...h.metadata,parent}))},chunks:[],file:undefined});
            }
        }
        await this.transaction(['revisions','chunks','state','deletions','work'],'readwrite',async tx=>{
            c.check(active());
            const records=tx.objectStore('revisions'),state=tx.objectStore('state'),chunks=tx.objectStore('chunks');
            const all:Stored[]=await req(records.getAll()),bases=new Set(all.map(r=>r.revision.base));
            const current=all.filter(r=>!bases.has(r.revision.id));
            const expected=new Set(heads.map(h=>h.revision.id));
            if(current.length!==expected.size||current.some(r=>!expected.has(r.revision.id)))throw Error('The library changed. Select the items again.');
            const origin=await req(state.get('relay-device')) as string;
            if(destination)c.check(!await req(tx.objectStore('deletions').get(destination)));
            let order=await req(state.get('order'))||0;
            for(const {r,file,chunks:content} of prepared){
                c.check(!await req(tx.objectStore('deletions').get(r.object)));
                for(let i=0;i<content.length;i++)chunks.add(content[i],file+':'+i);
                const old=!copy?all.find(record=>record.revision.id===r.base):undefined;
                const fileID=fileIdentity(r);
                records.add({revision:r,pending:true,metadataPending:true,metadataQueued:1,file:fileID||undefined,order:++order,origin,...(old?.deferred?{deferred:true,wanted:old.wanted,trash:old.trash}:{})} satisfies Stored);
                if(fileID)tx.objectStore('work').put(workTask(r,true));else if(r.base)state.put(true,'work-reconcile');
            }
            state.put(order,'order');c.check(active());
        });
        this.onChange?.();
    }
    async edit(master: Uint8Array, base: Revision, edit: Pick<c.Metadata, 'name' | 'parent' | 'tags' | 'favorite' | 'trashed_at' | 'trash_previous_parent' | 'folder_icon'>) {
        if(edit.parent)c.check(!await this.transaction(['deletions'],'readonly',tx=>req(tx.objectStore('deletions').get(edit.parent!))));
        const { metadata, key } = unlockRevision(master, this.account.account, base);
        if (key)
            c.wipe(key);
        if (edit.folder_icon !== undefined) c.check(metadata.type === 'folder' && c.validFolderIcon(edit.folder_icon));
        const next={...metadata,...edit};
        if(JSON.stringify(metadata)===JSON.stringify(next))return base;
        const r: Revision = { ...base, id: c.id(), base: base.id, metadata: c.base64(c.encryptMetadata(master, this.account.account, base.object, { ...metadata, ...edit })) };
        await this.commit(r, [], undefined, true,undefined,undefined,!(await this.contentAvailable(master,r)));
        return r;
    }
}
