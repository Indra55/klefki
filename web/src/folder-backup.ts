import * as c from './crypto.ts';
import {Store, storeReference, unlockRevision, type Stored} from './store.ts';
import type {Deletion} from './trash.ts';

export type Directory = FileSystemDirectoryHandle & {
    queryPermission(options: {mode: 'readwrite'}): Promise<PermissionState>;
    requestPermission(options: {mode: 'readwrite'}): Promise<PermissionState>;
    entries(): AsyncIterableIterator<[string, FileSystemHandle]>;
};
export function folderSupported() { return 'showDirectoryPicker' in window; }
export async function pickFolder(): Promise<Directory> {
    return (window as unknown as {showDirectoryPicker(options: {mode: string}): Promise<Directory>}).showDirectoryPicker({mode:'readwrite'});
}
export async function folderPermission(folder: Directory, ask = false) {
    if (await folder.queryPermission({mode:'readwrite'}) === 'granted') return true;
    return ask && await folder.requestPermission({mode:'readwrite'}) === 'granted';
}
const readRequest = <T>(r: IDBRequest<T>) => new Promise<T>((resolve,reject)=>{r.onsuccess=()=>resolve(r.result);r.onerror=()=>reject(r.error);});
const settingsKeys = ['sync-mode','trash-default','trash-override','sync-paused','device-aliases'] as const;
const MAX_CATALOGUE = 64 * 1024 * 1024;
const maxSnapshots = 1000;
export type BackupStatus = {folder?:string; completed?:number; incomplete?:boolean; pending?:boolean; error?:string; running?:boolean};
type Inventory = {name:string;size:number;hash:string};
type Catalogue = {
    format:'klefki-pwa-backup';version:1;account:string;created:number;
    records:Stored[];deletions:(Deletion & {pending:boolean})[];
    state:Record<string,unknown>;preferences:Record<string,unknown>;inventory:Inventory[];incomplete:boolean;
};
type Envelope = {version:1;account:c.Account;file:string;key:string;size:number;chunks:number;created:number};
type Completed = {name:string;envelope:Envelope;catalogue:Catalogue};
export class BackupReviewRequired extends Error { constructor(){super('Backup needs review: the local store is empty or incomplete compared with the last complete backup. Restore it, or explicitly save this state.');} }
const checkAbort = (signal:AbortSignal) => signal.throwIfAborted();
const digest = async (bytes:Uint8Array) => c.hex(new Uint8Array(await crypto.subtle.digest('SHA-256',bytes as Uint8Array<ArrayBuffer>)));
const chunkName = (file:string,i:number) => file + '-' + i + '.bin';
function validChunkName(name:string){c.check(/^[0-9a-f]{32}-(0|[1-9][0-9]*)\.bin$/.test(name));}
async function readFile(dir:FileSystemDirectoryHandle,name:string,limit:number){
    const file=await (await dir.getFileHandle(name)).getFile();c.check(file.size<=limit);
    return new Uint8Array(await file.arrayBuffer());
}
async function writeFile(dir:FileSystemDirectoryHandle,name:string,bytes:Uint8Array,signal:AbortSignal){
    checkAbort(signal);
    const writer=await (await dir.getFileHandle(name,{create:true})).createWritable();
    try{await writer.write(bytes as Uint8Array<ArrayBuffer>);checkAbort(signal);await writer.close();}
    catch(error){await writer.abort().catch(()=>{});throw error;}
}
function preferences(account:string):Record<string,unknown>{
    const out:Record<string,unknown>={};
    for(const key of ['klefki-appearance-v1','klefki-semantic-'+account]){
        const value=localStorage.getItem(key);
        if(value!==null)out[key]=c.strictJSON(value);
    }
    return out;
}
async function capture(store:Store):Promise<Omit<Catalogue,'created'|'inventory'|'incomplete'>>{
    const snapshot=await store.transaction(['state','revisions','deletions'],'readonly',async tx=>{
        const state:Record<string,unknown>={};
        const values=await Promise.all(settingsKeys.map(key=>readRequest(tx.objectStore('state').get(key))));
        settingsKeys.forEach((key,i)=>{if(values[i]!==undefined)state[key]=values[i];});
        return {state,records:await readRequest(tx.objectStore('revisions').getAll()) as Stored[],deletions:await readRequest(tx.objectStore('deletions').getAll()) as Catalogue['deletions']};
    });
    return {format:'klefki-pwa-backup',version:1,account:store.account.account,...snapshot,preferences:preferences(store.account.account)};
}
function validateCatalogue(value:Catalogue,account:string){
    c.check(value?.format==='klefki-pwa-backup'&&value.version===1&&value.account===account&&Number.isSafeInteger(value.created)&&value.created>0);
    c.check(Array.isArray(value.records)&&Array.isArray(value.deletions)&&Array.isArray(value.inventory)&&typeof value.incomplete==='boolean');
    c.check(value.state&&typeof value.state==='object'&&!Array.isArray(value.state)&&Object.keys(value.state).every(k=>settingsKeys.includes(k as typeof settingsKeys[number])));
    c.check(value.preferences&&typeof value.preferences==='object'&&!Array.isArray(value.preferences));
    c.check(Object.keys(value.preferences).every(k=>k==='klefki-appearance-v1'||k==='klefki-semantic-'+account));
    if(value.state['sync-mode']!==undefined)c.check(['persistent','temporary-relay'].includes(String(value.state['sync-mode'])));
    if(value.state['trash-override']!==undefined)c.check(['follow-vault','on','off'].includes(String(value.state['trash-override'])));
    for(const k of ['trash-default','sync-paused'])if(value.state[k]!==undefined)c.check(typeof value.state[k]==='boolean');
    const ids=new Set<string>();let order=0;
    for(const record of value.records){
        c.check(record&&Number.isSafeInteger(record.order)&&record.order>order&&typeof record.pending==='boolean');order=record.order;
        c.check(!ids.has(record.revision.id));ids.add(record.revision.id);
        for(const key of ['deferred','wanted','explicit','trash','metadataPending','uploadPending'] as const)if(record[key]!==undefined)c.check(typeof record[key]==='boolean');
    }
    const deleted=new Set<string>();
    for(const d of value.deletions){c.validID(d.object);c.check(!deleted.has(d.object)&&typeof d.folder==='boolean'&&typeof d.pending==='boolean'&&Array.isArray(d.files));d.files.forEach(c.validID);deleted.add(d.object);}
    c.check(value.records.every(r=>!deleted.has(r.revision.object)));
    const files=new Set<string>();
    for(const file of value.inventory){validChunkName(file.name);c.check(!files.has(file.name)&&Number.isSafeInteger(file.size)&&file.size>=87&&file.size<=c.CHUNK+87&&/^[0-9a-f]{64}$/.test(file.hash));files.add(file.name);}
}
async function readCatalogue(dir:FileSystemDirectoryHandle,name:string,master:Uint8Array,account:c.Account,signal?:AbortSignal):Promise<Completed>{
    signal?.throwIfAborted();
    c.check(/^snapshot-[0-9]+-[0-9a-f]{32}\.json$/.test(name));
    const envelope=c.strictJSON(c.decode(await readFile(dir,name,8192))) as Envelope;
    c.check(envelope.version===1&&c.hex(c.accountBytes(c.validateAccount(envelope.account)))===c.hex(c.accountBytes(account)));
    c.validID(envelope.file);c.check(Number.isSafeInteger(envelope.size)&&envelope.size>=0&&envelope.size<=MAX_CATALOGUE&&envelope.chunks===c.chunkCount(envelope.size));
    const key=c.open(master,{kind:3,account:account.account,object:envelope.file},c.unbase64(envelope.key));
    const encrypted:Uint8Array[]=[];
    try{
        for(let i=0;i<envelope.chunks;i++){signal?.throwIfAborted();encrypted.push(await readFile(dir,chunkName(envelope.file,i),c.CHUNK+87));}
        signal?.throwIfAborted();
        const bytes=c.decryptFile(key,account.account,envelope.file,envelope.size,encrypted);
        try{
            const catalogue=c.strictJSON(c.decode(bytes)) as Catalogue;validateCatalogue(catalogue,account.account);
            c.check(catalogue.created===envelope.created);
            return {name,envelope,catalogue};
        }finally{c.wipe(bytes);}
    }finally{c.wipe(key);}
}
export async function snapshots(dir:Directory,master:Uint8Array,account:c.Account,signal?:AbortSignal):Promise<{valid:Completed[];invalid:number}>{
    const names:string[]=[];
    for await(const [name,handle] of dir.entries())if(handle.kind==='file'&&/^snapshot-[0-9]+-[0-9a-f]{32}\.json$/.test(name)){names.push(name);c.check(names.length<=maxSnapshots);}
    const valid:Completed[]=[];let invalid=0;
    for(const name of names){signal?.throwIfAborted();try{valid.push(await readCatalogue(dir,name,master,account,signal));}catch{signal?.throwIfAborted();invalid++;}}
    valid.sort((a,b)=>b.catalogue.created-a.catalogue.created||b.name.localeCompare(a.name));return {valid,invalid};
}
async function verifyContent(catalogue:Catalogue,master:Uint8Array,read:(name:string)=>Promise<Uint8Array>,signal:AbortSignal,consume?:(file:string,index:number,bytes:Uint8Array)=>Promise<void>){
    const inventory=new Map(catalogue.inventory.map(x=>[x.name,x]));
    const parents=new Map<string,string>(),used=new Set<string>();let incomplete=false;
    for(const record of catalogue.records){
        checkAbort(signal);
        const r=record.revision,{metadata:m,key}=unlockRevision(master,catalogue.account,r);
        try{
            c.check(!r.base||parents.get(r.base)===r.object);parents.set(r.id,r.object);
            if(!key)continue;
            if(record.deferred){incomplete=true;continue;}
            for(let i=0;i<m.chunk_count!;i++){
                checkAbort(signal);const name=chunkName(m.file_id!,i),expected=inventory.get(name);c.check(expected);
                if(used.has(name))continue;
                const bytes=await read(name);c.check(bytes.length===expected.size&&await digest(bytes)===expected.hash);
                const plain=c.open(key,{kind:5,account:catalogue.account,object:m.file_id!,index:i,final:i===m.chunk_count!-1},bytes);
                try{c.check(plain.length===Math.min(c.CHUNK,m.size!-i*c.CHUNK));}finally{c.wipe(plain);}
                await consume?.(m.file_id!,i,bytes);used.add(name);
            }
        }finally{if(key)c.wipe(key);}
    }
    c.check(used.size===inventory.size&&incomplete===catalogue.incomplete);
}
export async function writeSnapshot(store:Store,master:Uint8Array,dir:Directory,signal:AbortSignal,allowIncomplete=false){
    const snapshot=await capture(store);snapshot.records.sort((a,b)=>a.order-b.order);
    const catalogue:Catalogue={...snapshot,created:Date.now(),inventory:[],incomplete:snapshot.records.some(r=>r.deferred===true)};
    const previous=await snapshots(dir,master,store.account,signal);
    if(previous.invalid&&!previous.valid.length)throw Error('Existing backups could not be verified. Choose another folder or restore a valid snapshot.');
    if(!allowIncomplete&&previous.valid.some(x=>!x.catalogue.incomplete&&x.catalogue.records.length>0)&&(catalogue.incomplete||!catalogue.records.length))throw new BackupReviewRequired();
    const known=new Set<string>();
    for(const record of catalogue.records){
        const {metadata:m,key}=unlockRevision(master,store.account.account,record.revision);
        try {
        if(m.type!=='file'||record.deferred)continue;
        for(let i=0;i<m.chunk_count!;i++){
            checkAbort(signal);const name=chunkName(m.file_id!,i);if(known.has(name))continue;
            const bytes=await store.chunk(m.file_id!,i);if(!bytes)throw Error('A local file is missing. The previous backup has been kept.');
            const plain=c.open(key!,{kind:5,account:store.account.account,object:m.file_id!,index:i,final:i===m.chunk_count!-1},bytes);
            try{c.check(plain.length===Math.min(c.CHUNK,m.size!-i*c.CHUNK));}finally{c.wipe(plain);}
            const hash=await digest(bytes);let reuse=false;
            try{c.check(await digest(await readFile(dir,name,c.CHUNK+87))===hash);reuse=true;}catch(error){if(!(error instanceof DOMException&&error.name==='NotFoundError'))throw error;}
            if(!reuse)await writeFile(dir,name,bytes,signal);
            catalogue.inventory.push({name,size:bytes.length,hash});known.add(name);
        }
        }finally{if(key)c.wipe(key);}
    }
    validateCatalogue(catalogue,store.account.account);
    await verifyContent(catalogue,master,name=>readFile(dir,name,c.CHUNK+87),signal);
    const bytes=c.utf8(JSON.stringify(catalogue));
    const file=c.id(),key=c.random(32),name='snapshot-'+catalogue.created+'-'+c.id()+'.json';
    try{
        c.check(bytes.length<=MAX_CATALOGUE);
        const envelope:Envelope={version:1,account:store.account,file,key:c.base64(c.seal(master,{kind:3,account:store.account.account,object:file},key)),size:bytes.length,chunks:c.chunkCount(bytes.length),created:catalogue.created};
        for(let i=0;i<envelope.chunks;i++)await writeFile(dir,chunkName(file,i),c.seal(key,{kind:5,account:store.account.account,object:file,index:i,final:i===envelope.chunks-1},bytes.subarray(i*c.CHUNK,(i+1)*c.CHUNK)),signal);
        // The envelope is the commit record. Incomplete snapshots never replace a completed one.
        await writeFile(dir,name,c.utf8(JSON.stringify(envelope)),signal);
        const completed=await readCatalogue(dir,name,master,store.account,signal);
        const retained=[completed,...previous.valid].slice(0,2),keep=new Set(retained.flatMap(s=>[s.name,...s.catalogue.inventory.map(x=>x.name),...Array.from({length:s.envelope.chunks},(_,i)=>chunkName(s.envelope.file,i))]));
        // Unknown or invalid files are left alone for recovery and investigation.
        if(!previous.invalid){
            for(const old of previous.valid.slice(1)){
                checkAbort(signal);await dir.removeEntry(old.name);
            }
            for await(const [entry,handle] of dir.entries())if(handle.kind==='file'&&/^[0-9a-f]{32}-(0|[1-9][0-9]*)\.bin$/.test(entry)&&!keep.has(entry)){checkAbort(signal);await dir.removeEntry(entry);}
        }
        return {completed:catalogue.created,incomplete:catalogue.incomplete};
    }finally{c.wipe(bytes);c.wipe(key);}
}
export async function backupAccount(dir:FileSystemDirectoryHandle):Promise<c.Account>{return c.parseAccount(await readFile(dir,'account.klefki',260));}
export async function prepareFolder(root:Directory,account:c.Account){
    const dir=await root.getDirectoryHandle('klefki-'+account.account,{create:true}) as Directory;
    try{c.check(c.hex(c.accountBytes(await backupAccount(dir)))===c.hex(c.accountBytes(account)));}
    catch(error){if(!(error instanceof DOMException&&error.name==='NotFoundError'))throw error;await writeFile(dir,'account.klefki',c.accountBytes(account),new AbortController().signal);}
    return dir;
}
export async function restoreSnapshot(dir:Directory,account:c.Account,master:Uint8Array,name:string,signal:AbortSignal){
    const snapshot=await readCatalogue(dir,name,master,account,signal),catalogue=snapshot.catalogue;
    const stage=await Store.open(account,'klefki-v1-'+account.account+'-restore-'+c.id());
    try{
        await verifyContent(catalogue,master,file=>readFile(dir,file,c.CHUNK+87),signal,async(file,index,bytes)=>{
            await stage.transaction(['chunks'],'readwrite',async tx=>{tx.objectStore('chunks').put(bytes,file+':'+index);});
        });
        checkAbort(signal);
        await stage.transaction(['state','revisions','deletions'],'readwrite',async tx=>{
            const state=tx.objectStore('state');
            for(const record of catalogue.records){const {sequence,...local}=record;tx.objectStore('revisions').put({...local,pending:true,metadataPending:true});}
            for(const deletion of catalogue.deletions)tx.objectStore('deletions').put({...deletion,pending:true});
            for(const [key,value] of Object.entries(catalogue.state))state.put(value,key);
            state.put(catalogue.records.at(-1)?.order||0,'order');state.put(true,'sync-paused');
            state.put(catalogue.preferences,'restored-preferences');
            state.put({completed:catalogue.created,incomplete:catalogue.incomplete,folder:dir.name},'backup-status');
            state.put(dir,'backup-folder');
        });
        return stage;
    }catch(error){const name=stage.db.name;stage.close();indexedDB.deleteDatabase(name);throw error;}
}
export function activateRestoredStore(store:Store){
    // The single active-store reference is published only after the staging store is verified.
    const previous=localStorage.getItem('klefki-account');
    localStorage.setItem('klefki-account',JSON.stringify(store.account));
    try{localStorage.setItem(storeReference(store.account.account),store.db.name);}
    catch(error){if(previous===null)localStorage.removeItem('klefki-account');else localStorage.setItem('klefki-account',previous);throw error;}
}
export async function applyRestoredPreferences(store:Store){
    const saved=await store.state<Record<string,unknown>>('restored-preferences');if(!saved)return;
    for(const key of ['klefki-appearance-v1','klefki-semantic-'+store.account.account]){if(Object.hasOwn(saved,key))localStorage.setItem(key,JSON.stringify(saved[key]));else localStorage.removeItem(key);}
    await store.transaction(['state'],'readwrite',async tx=>{tx.objectStore('state').delete('restored-preferences');});
    window.dispatchEvent(new Event('klefki-preferences'));
}

export class FolderBackup {
    status:BackupStatus={};
    private folder?:Directory;
    private timer?:number;
    private firstDirty=0;
    private revision=0;
    private active?:Promise<void>;
    private controller=new AbortController();
    private fingerprint='';
    private changed=()=>this.dirty();
    constructor(private store:Store,private master:Uint8Array,private render:()=>void){}
    async start(){
        this.folder=await this.store.state<Directory>('backup-folder');
        this.status=await this.store.state<BackupStatus>('backup-status')||{};
        if(this.folder)this.status={...this.status,folder:this.folder.name};
        this.store.onMutation=this.changed;window.addEventListener('klefki-preferences',this.changed);
        if(this.folder)this.dirty();else this.render();
    }
    stop(){this.controller.abort();clearTimeout(this.timer);this.store.onMutation=undefined;window.removeEventListener('klefki-preferences',this.changed);}
    dirty(){
        if(!this.folder||this.controller.signal.aborted)return;
        this.revision++;this.firstDirty ||= Date.now();this.status={...this.status,pending:true};this.render();
        clearTimeout(this.timer);this.timer=window.setTimeout(()=>void this.run(),Math.max(0,Math.min(5000,this.firstDirty+30000-Date.now())));
    }
    async choose(root:Directory){
        if(!await folderPermission(root,true))throw Error('Folder permission is required to save a backup.');
        if(this.active)await this.active;
        checkAbort(this.controller.signal);
        this.folder=await prepareFolder(root,this.store.account);this.fingerprint='';
        await this.store.transaction(['state'],'readwrite',async tx=>{tx.objectStore('state').put(this.folder,'backup-folder');},false);
        this.status={folder:this.folder.name,pending:true};await this.run(true);
    }
    async reconnect(){
        if(this.folder&&!await folderPermission(this.folder,true))throw Error('Folder permission was not granted.');
        await this.run(true);
    }
    async run(force=false,reviewed=false):Promise<void>{
        clearTimeout(this.timer);
        if(this.active){await this.active;if(force)return this.run(force,reviewed);return;}
        if(!this.folder||this.controller.signal.aborted)return;
        const revision=this.revision,folder=this.folder;this.firstDirty=0;
        this.active=(async()=>{
            let key:Uint8Array|undefined;
            try{
                if(!await folderPermission(folder))throw Error('Reconnect the backup folder to grant access.');
                const fingerprint=await digest(c.utf8(JSON.stringify(await capture(this.store))));
                if(!force&&fingerprint===this.fingerprint){this.status={...this.status,pending:false,error:undefined};return;}
                this.status={...this.status,running:true,error:undefined};this.render();
                checkAbort(this.controller.signal);key=this.master.slice();
                const result=await navigator.locks.request('klefki-backup-'+this.store.account.account,()=>writeSnapshot(this.store,key!,folder,this.controller.signal,reviewed));
                this.fingerprint=fingerprint;this.status={folder:folder.name,...result,pending:revision!==this.revision};
                await this.store.transaction(['state'],'readwrite',async tx=>{tx.objectStore('state').put(this.status,'backup-status');},false);
            }catch(error){if(!this.controller.signal.aborted)this.status={...this.status,running:false,pending:true,error:error instanceof Error?error.message:'Backup failed. The last completed copy is kept.'};}
            finally{if(key)c.wipe(key);this.status.running=false;if(!this.controller.signal.aborted)this.render();}
        })();
        try{await this.active;}finally{this.active=undefined;if(!this.controller.signal.aborted&&revision!==this.revision)this.dirty();}
    }
}
