import {test,expect,type Page} from '@playwright/test';
import * as c from '../src/crypto.ts';
import {openSettings,upload,documentAction} from './ui.ts';
const password='public offline backup test password';
async function unlock(page:Page){await page.getByLabel('Password',{exact:true}).fill(password);await page.getByRole('button',{name:'Unlock Klefki',exact:true}).click();await expect(page.getByRole('heading',{name:'Documents',exact:true})).toBeVisible();}
async function fixture(page:Page){
    await c.ready;const created=c.createAccount(password);c.wipe(created.master);
    // OPFS supplies real file handles to the picker boundary. It is only a browser-test stand-in for an external folder.
    await page.addInitScript(()=>{
        (window as any).showDirectoryPicker=async()=>{
            const root=await (await navigator.storage.getDirectory()).getDirectoryHandle('test-external-backup',{create:true});
            return (window as any).restoreFolder?root.getDirectoryHandle((window as any).restoreFolder):root;
        };
    });
    await page.goto('/');
    await page.getByLabel('Open an encrypted account file').setInputFiles({name:'account.klefki',mimeType:'application/octet-stream',buffer:Buffer.from(c.accountBytes(created.account))});
    await unlock(page);await openSettings(page,'Sync & devices');await page.getByRole('button',{name:'Pause sync',exact:true}).click();
    await upload(page,{name:'offline-secret.txt',mimeType:'text/plain',buffer:Buffer.from('offline bytes never need another device')});
    await expect(page.getByRole('button',{name:'Open offline-secret.txt',exact:true})).toBeVisible();
    return created.account;
}
async function chooseBackup(page:Page){await openSettings(page,'Offline & backup');await page.getByRole('button',{name:'Choose offline backup folder',exact:true}).click();await expect(page.locator('[data-backup-status]')).toContainText('Backup is up to date.',{timeout:30000});}
async function runNow(page:Page){const previous=Number(await page.locator('[data-backup-status]').getAttribute('data-completed'));await page.getByRole('button',{name:'Back up now',exact:true}).click();await expect.poll(async()=>Number(await page.locator('[data-backup-status]').getAttribute('data-completed')),{timeout:30000}).toBeGreaterThan(previous);await expect(page.locator('[data-backup-status]')).toContainText('Backup is up to date.');}
async function syncBackup(page:Page){await openSettings(page,'Offline & backup');await runNow(page);}
async function wipeBrowserStore(page:Page){
    await page.getByRole('button',{name:'Lock',exact:true}).click();
    await page.evaluate(async()=>{for(const db of await indexedDB.databases())if(db.name)await new Promise<void>((resolve,reject)=>{const q=indexedDB.deleteDatabase(db.name!);q.onsuccess=()=>resolve();q.onerror=()=>reject(q.error);});localStorage.clear();});
    await page.reload();
}
async function readBackups(page:Page,account:c.Account){
    await page.evaluate(id=>{(window as any).restoreFolder='klefki-'+id;},account.account);
    await page.getByRole('button',{name:'Restore from backup folder',exact:true}).click();
    await page.getByLabel('Backup password',{exact:true}).fill(password);
    await page.getByRole('button',{name:'Read backups',exact:true}).click();
    await expect(page.getByLabel('Backup snapshot',{exact:true})).toBeVisible();
}
async function finishRestore(page:Page){await page.getByRole('button',{name:'Restore selected backup',exact:true}).click();await expect(page.locator('#status')).toContainText('Backup restored.');await unlock(page);}

test('encrypted folder restores documents and settings offline after browser database loss',async({page,context})=>{
    const account=await fixture(page);
    await openSettings(page,'Appearance');await page.getByLabel('Color theme',{exact:true}).selectOption('light');
    await chooseBackup(page);
    const files=await page.evaluate(async id=>{
        const root=await (await navigator.storage.getDirectory()).getDirectoryHandle('test-external-backup');const dir=await root.getDirectoryHandle('klefki-'+id);const values:string[]=[];
        for await(const handle of (dir as any).values())if(handle.kind==='file')values.push(await (await handle.getFile()).text());return values.join('\n');
    },account.account);
    for(const secret of [password,'offline-secret.txt','offline bytes never need another device','klefki-appearance-v1'])expect(files).not.toContain(secret);
    await page.evaluate(()=>navigator.serviceWorker.ready);await expect.poll(()=>page.evaluate(()=>!!navigator.serviceWorker.controller)).toBe(true);
    await context.setOffline(true);await wipeBrowserStore(page);await readBackups(page,account);await finishRestore(page);
    await expect(page.locator('html')).toHaveAttribute('data-theme','light');
    await page.getByRole('button',{name:'Open offline-secret.txt',exact:true}).click();await expect(page.locator('dialog pre')).toHaveText('offline bytes never need another device');
    await page.getByRole('button',{name:'Close',exact:true}).click();await openSettings(page,'Sync & devices');await expect(page.getByRole('main').getByRole('button',{name:'Resume sync',exact:true})).toBeVisible();
    await page.reload();await unlock(page);await expect(page.getByRole('button',{name:'Open offline-secret.txt',exact:true})).toBeVisible();
});

test('automatic backup captures later changes and retains two snapshots',async({page})=>{
    const account=await fixture(page);await chooseBackup(page);
    await upload(page,{name:'second.txt',mimeType:'text/plain',buffer:Buffer.from('second automatic copy')});
    await openSettings(page,'Offline & backup');await expect(page.locator('[data-backup-status]')).toContainText('Backup is up to date.',{timeout:30000});
    await runNow(page);
    await expect.poll(()=>page.evaluate(async id=>{const dir=await (await (await navigator.storage.getDirectory()).getDirectoryHandle('test-external-backup')).getDirectoryHandle('klefki-'+id);let count=0;for await(const [name] of (dir as any).entries())if(name.startsWith('snapshot-'))count++;return count;},account.account)).toBe(2);
    await wipeBrowserStore(page);await readBackups(page,account);await finishRestore(page);
    await page.getByRole('button',{name:'Open second.txt',exact:true}).click();await expect(page.locator('dialog pre')).toHaveText('second automatic copy');
});

test('failed folder writes preserve the completed backup and report pending changes',async({page})=>{
    const account=await fixture(page);await chooseBackup(page);
    await page.evaluate(()=>{const original=FileSystemFileHandle.prototype.createWritable;(window as any).originalWriter=original;FileSystemFileHandle.prototype.createWritable=async()=>{throw new DOMException('Disk full','QuotaExceededError');};});
    await upload(page,{name:'unsaved-backup.txt',mimeType:'text/plain',buffer:Buffer.from('kept in browser')});
    await openSettings(page,'Offline & backup');await expect(page.locator('[data-backup-status]')).toContainText('Disk full',{timeout:30000});
    await page.evaluate(()=>{FileSystemFileHandle.prototype.createWritable=(window as any).originalWriter;});
    await wipeBrowserStore(page);await readBackups(page,account);await finishRestore(page);
    await expect(page.getByRole('button',{name:'Open offline-secret.txt',exact:true})).toBeVisible();
    await expect(page.getByRole('button',{name:'Open unsaved-backup.txt',exact:true})).toHaveCount(0);
});

test('tampered latest catalogue falls back to the previous snapshot',async({page})=>{
    const account=await fixture(page);await chooseBackup(page);
    await upload(page,{name:'later.txt',mimeType:'text/plain',buffer:Buffer.from('later')});await syncBackup(page);
    await page.getByRole('button',{name:'Lock',exact:true}).click();
    await page.evaluate(async id=>{
        const dir=await (await (await navigator.storage.getDirectory()).getDirectoryHandle('test-external-backup')).getDirectoryHandle('klefki-'+id);const snapshots:string[]=[];
        for await(const [name] of (dir as any).entries())if(name.startsWith('snapshot-'))snapshots.push(name);snapshots.sort().reverse();
        const envelope=JSON.parse(await (await (await dir.getFileHandle(snapshots[0])).getFile()).text());const handle=await dir.getFileHandle(envelope.file+'-0.bin');const writer=await handle.createWritable();await writer.write(new Uint8Array([1,2,3]));await writer.close();
    },account.account);
    await readBackups(page,account);await expect(page.locator('dialog .status')).toContainText('Some snapshots could not be verified');await finishRestore(page);
    await expect(page.getByRole('button',{name:'Open offline-secret.txt',exact:true})).toBeVisible();await expect(page.getByRole('button',{name:'Open later.txt',exact:true})).toHaveCount(0);
});

test('missing local database is recovery state and unknown mode is not persistent mode',async({page})=>{
    const account=await fixture(page);await page.getByRole('button',{name:'Lock',exact:true}).click();
    await page.evaluate(async id=>{await new Promise<void>((resolve,reject)=>{const q=indexedDB.deleteDatabase('klefki-v1-'+id);q.onsuccess=()=>resolve();q.onerror=()=>reject(q.error);});},account.account);
    await unlock(page);await expect(page.getByRole('heading',{name:'Local copy unavailable.',exact:true})).toBeVisible();
    await openSettings(page,'Sync & devices');await expect(page.getByLabel('Sync mode',{exact:true})).toHaveValue('');await expect(page.getByRole('main').getByRole('button',{name:'Resume sync',exact:true})).toBeVisible();
});

test('missing chunks stay visible and are not reported as available offline',async({page})=>{
    const account=await fixture(page);await page.getByRole('button',{name:'Lock',exact:true}).click();
    await page.evaluate(async id=>{const db=await new Promise<IDBDatabase>((resolve,reject)=>{const q=indexedDB.open('klefki-v1-'+id);q.onsuccess=()=>resolve(q.result);q.onerror=()=>reject(q.error);});await new Promise<void>((resolve,reject)=>{const tx=db.transaction('chunks','readwrite');tx.objectStore('chunks').clear();tx.oncomplete=()=>resolve();tx.onabort=()=>reject(tx.error);});db.close();},account.account);
    await unlock(page);await expect(page.locator('article')).toContainText('Not stored on this device');
    await page.getByRole('button',{name:'Open offline-secret.txt',exact:true}).click();await expect(page.locator('#status')).toContainText('not stored on this device');
});

test('denied persistence does not prevent offline use',async({page,context})=>{
    await page.addInitScript(()=>{Object.defineProperty(navigator.storage,'persisted',{value:async()=>false});Object.defineProperty(navigator.storage,'persist',{value:async()=>false});});
    await fixture(page);await openSettings(page,'Offline & backup');await expect(page.locator('[data-storage-protection]')).toContainText('not granted');
    await page.evaluate(()=>navigator.serviceWorker.ready);await expect.poll(()=>page.evaluate(()=>!!navigator.serviceWorker.controller)).toBe(true);await context.setOffline(true);await page.reload();await unlock(page);
    await page.getByRole('button',{name:'Open offline-secret.txt',exact:true}).click();await expect(page.locator('dialog pre')).toHaveText('offline bytes never need another device');
});

test('revoked folder permission keeps the previous backup and can be reauthorized',async({page})=>{
    await fixture(page);await chooseBackup(page);
    await page.evaluate(()=>{
        (window as any).originalPermission=FileSystemDirectoryHandle.prototype.queryPermission;
        FileSystemDirectoryHandle.prototype.queryPermission=async()=> 'prompt';
        FileSystemDirectoryHandle.prototype.requestPermission=async()=> 'denied';
    });
    await upload(page,{name:'permission.txt',mimeType:'text/plain',buffer:Buffer.from('pending folder permission')});
    await openSettings(page,'Offline & backup');await expect(page.locator('[data-backup-status]')).toContainText('Reconnect the backup folder',{timeout:30000});
    await page.getByRole('button',{name:'Back up now',exact:true}).click();await expect(page.locator('#status')).toContainText('not granted');
    await page.evaluate(()=>{FileSystemDirectoryHandle.prototype.queryPermission=(window as any).originalPermission;});
    await page.getByRole('button',{name:'Back up now',exact:true}).click();await expect(page.locator('[data-backup-status]')).toContainText('Backup is up to date.');
});

test('persistent sync reauthenticates once without hiding offline files',async({page})=>{
    const account=await fixture(page);const keys=c.unlock(account,password);
    try{
        const response=await page.request.post('/v1/signup',{data:{...account,credential:c.base64(keys.credential)}});expect(response.ok()).toBe(true);
        await openSettings(page,'Sync & devices');await page.getByRole('main').getByRole('button',{name:'Resume sync',exact:true}).click();await expect(page.locator('#status')).toContainText('Synced.');
        let rejected=false,logins=0;
        await page.route('**/v1/login',async route=>{logins++;await route.continue();});
        await page.route('**/v1/sync/check',async route=>{if(!rejected){rejected=true;await route.fulfill({status:401,body:''});}else await route.continue();});
        await page.getByRole('button',{name:'Sync now',exact:true}).click();await expect.poll(()=>rejected).toBe(true);await expect(page.locator('#status')).toContainText('Synced.');expect(logins).toBe(1);
        await page.getByRole('button',{name:'Back to library',exact:true}).click();await page.getByRole('button',{name:'Open offline-secret.txt',exact:true}).click();await expect(page.locator('dialog pre')).toHaveText('offline bytes never need another device');
    }finally{c.wipe(keys.master);c.wipe(keys.credential);}
});

test('confirmed relay mode survives a failure in subsequent sync',async({page})=>{
    const account=await fixture(page),keys=c.unlock(account,password);
    try{
        expect((await page.request.post('/v1/signup',{data:{...account,credential:c.base64(keys.credential)}})).ok()).toBe(true);
        await openSettings(page,'Sync & devices');await page.getByRole('main').getByRole('button',{name:'Resume sync',exact:true}).click();await expect(page.locator('#status')).toContainText('Synced.');
        await page.getByLabel('Sync mode',{exact:true}).selectOption('temporary-relay');
        await page.route('**/v1/sync/settings',async route=>{if(route.request().method()==='GET')await route.fulfill({status:503,body:''});else await route.continue();});
        await page.getByRole('button',{name:'Apply sync mode',exact:true}).click();await expect(page.locator('#status')).toContainText('Server request failed');
        await expect(page.getByLabel('Sync mode',{exact:true})).toHaveValue('temporary-relay');
        await page.getByRole('button',{name:'Lock',exact:true}).click();await page.context().setOffline(true);await unlock(page);await openSettings(page,'Sync & devices');await expect(page.getByLabel('Sync mode',{exact:true})).toHaveValue('temporary-relay');
    }finally{c.wipe(keys.master);c.wipe(keys.credential);}
});

test('an unexpectedly empty store cannot automatically replace its complete backup',async({page})=>{
    const account=await fixture(page);await chooseBackup(page);await page.getByRole('button',{name:'Lock',exact:true}).click();
    await page.evaluate(async id=>{const db=await new Promise<IDBDatabase>(resolve=>{const q=indexedDB.open('klefki-v1-'+id);q.onsuccess=()=>resolve(q.result);});await new Promise<void>((resolve,reject)=>{const tx=db.transaction(['revisions','chunks'],'readwrite');tx.objectStore('revisions').clear();tx.objectStore('chunks').clear();tx.oncomplete=()=>resolve();tx.onabort=()=>reject(tx.error);});db.close();},account.account);
    await unlock(page);await openSettings(page,'Offline & backup');await expect(page.locator('[data-backup-status]')).toContainText('Backup needs review',{timeout:30000});
    await readBackups(page,account);await finishRestore(page);await expect(page.getByRole('button',{name:'Open offline-secret.txt',exact:true})).toBeVisible();
});

test('corrupt backup file fails staging and leaves the active store intact',async({page})=>{
    const account=await fixture(page);await chooseBackup(page);
    await page.getByRole('button',{name:'Lock',exact:true}).click();
    await page.evaluate(async id=>{
        const db=await new Promise<IDBDatabase>(resolve=>{const q=indexedDB.open('klefki-v1-'+id);q.onsuccess=()=>resolve(q.result);});
        const chunkKey=await new Promise<IDBValidKey>(resolve=>{const q=db.transaction('chunks').objectStore('chunks').getAllKeys();q.onsuccess=()=>resolve(q.result[0]);});db.close();
        const dir=await (await (await navigator.storage.getDirectory()).getDirectoryHandle('test-external-backup')).getDirectoryHandle('klefki-'+id);
        const handle=await dir.getFileHandle(String(chunkKey).replace(':','-')+'.bin');const original=new Uint8Array(await (await handle.getFile()).arrayBuffer());original[original.length-1]^=1;const writer=await handle.createWritable();await writer.write(original);await writer.close();
    },account.account);
    await readBackups(page,account);await page.getByRole('button',{name:'Restore selected backup',exact:true}).click();await expect(page.locator('dialog .status')).toContainText('Restore failed. The previous local store is kept.');
    await page.getByRole('button',{name:'Cancel',exact:true}).click();await unlock(page);await page.getByRole('button',{name:'Open offline-secret.txt',exact:true}).click();await expect(page.locator('dialog pre')).toHaveText('offline bytes never need another device');
});

test('locking interrupts an in-flight backup without publishing a partial snapshot',async({page})=>{
    const account=await fixture(page);await chooseBackup(page);
    await page.evaluate(()=>{
        const original=FileSystemFileHandle.prototype.createWritable;
        FileSystemFileHandle.prototype.createWritable=async function(...args){
            const stream=await original.apply(this,args);
            if(this.name.startsWith('snapshot-')){(window as any).backupWriting=true;await new Promise<void>(resolve=>{(window as any).releaseBackup=resolve;});}
            return stream;
        };
    });
    await upload(page,{name:'interrupted.txt',mimeType:'text/plain',buffer:Buffer.from('interrupted backup')});
    await expect.poll(()=>page.evaluate(()=>(window as any).backupWriting),{timeout:30000}).toBe(true);
    await page.getByRole('button',{name:'Lock',exact:true}).click();await page.evaluate(()=>(window as any).releaseBackup());
    await readBackups(page,account);await finishRestore(page);await expect(page.getByRole('button',{name:'Open offline-secret.txt',exact:true})).toBeVisible();await expect(page.getByRole('button',{name:'Open interrupted.txt',exact:true})).toHaveCount(0);
});

test('restore respects the account lock held by another tab',async({page,context})=>{
    const account=await fixture(page);await chooseBackup(page);
    const other=await context.newPage();await other.goto('/');
    await other.evaluate(()=>{(window as any).showDirectoryPicker=async()=>{const root=await (await navigator.storage.getDirectory()).getDirectoryHandle('test-external-backup');return root.getDirectoryHandle((window as any).restoreFolder);};});
    await readBackups(other,account);await other.getByRole('button',{name:'Restore selected backup',exact:true}).click();
    await expect(other.locator('dialog .status')).toContainText('Klefki open elsewhere');
    await page.getByRole('button',{name:'Back to library',exact:true}).click();await page.getByRole('button',{name:'Open offline-secret.txt',exact:true}).click();await expect(page.locator('dialog pre')).toHaveText('offline bytes never need another device');
    await other.close();
});

test('folder recovery verifies multiple chunks without another device',async({page})=>{
    const account=await fixture(page);
    const bytes=Buffer.alloc(c.CHUNK+19);for(let i=0;i<bytes.length;i++)bytes[i]=i%251;
    await upload(page,{name:'large.bin',mimeType:'application/octet-stream',buffer:bytes});await chooseBackup(page);
    await wipeBrowserStore(page);await readBackups(page,account);await finishRestore(page);
    const downloaded=page.waitForEvent('download');
    const row=page.locator('article').filter({has:page.getByText('large.bin',{exact:true})});await row.getByRole('button',{name:'Actions for large.bin',exact:true}).click();await page.getByRole('menuitem',{name:'Export',exact:true}).click();
    const {readFile}=await import('node:fs/promises');expect(await readFile((await (await downloaded).path())!)).toEqual(bytes);
});


test('folder backup preserves revision history, pending edits, folders and deletion records',async({page})=>{
    const account=await fixture(page);
    await page.getByRole('button',{name:'New folder',exact:true}).click();await page.getByLabel('Folder name',{exact:true}).fill('Private records');await page.getByRole('button',{name:'Create folder',exact:true}).click();
    await expect(page.getByRole('button',{name:'Open Private records',exact:true})).toBeVisible();
    await documentAction(page,'offline-secret.txt','Edit details & move');
    await page.getByLabel('Name',{exact:true}).fill('renamed.txt');await page.getByLabel('Tags, separated by commas').fill('Personal');await page.getByLabel('Favorite',{exact:true}).check();await page.getByLabel('Folder',{exact:true}).selectOption({label:'Private records'});await page.getByRole('button',{name:'Save changes',exact:true}).click();
    await expect(page.locator('#status')).toContainText('Changes saved locally.');
    await upload(page,{name:'removed.txt',mimeType:'text/plain',buffer:Buffer.from('deleted data must not appear in backup')});
    await documentAction(page,'removed.txt','Move to Trash');await expect(page.locator('#status')).toContainText('Document moved to Trash.');
    await page.getByRole('button',{name:'View',exact:true}).click();await page.getByRole('menuitem',{name:'Trash',exact:true}).click();await documentAction(page,'removed.txt','Delete permanently');
    await page.getByRole('dialog',{name:'Delete permanently?'}).getByRole('button',{name:'Delete permanently',exact:true}).click();await expect(page.getByText('removed.txt',{exact:true})).toHaveCount(0);
    async function localState(){return page.evaluate(async id=>{
        const name=localStorage.getItem('klefki-store-'+id)||'klefki-v1-'+id;
        const db=await new Promise<IDBDatabase>(resolve=>{const q=indexedDB.open(name);q.onsuccess=()=>resolve(q.result);});
        const tx=db.transaction(['revisions','deletions','state']);const read=<T,>(q:IDBRequest<T>)=>new Promise<T>(resolve=>{q.onsuccess=()=>resolve(q.result);});
        const values=await Promise.all([read(tx.objectStore('revisions').getAll()),read(tx.objectStore('deletions').getAll()),read(tx.objectStore('state').get('relay-device'))]);db.close();return {revisions:values[0],deletions:values[1],device:values[2]};
    },account.account);}
    const previous=await localState();expect(previous.revisions.length).toBeGreaterThan(2);expect(previous.deletions.length).toBeGreaterThan(0);
    await chooseBackup(page);await wipeBrowserStore(page);await readBackups(page,account);await finishRestore(page);
    const restored=await localState();expect(restored.revisions.map((r:any)=>r.revision)).toEqual(previous.revisions.map((r:any)=>r.revision));expect(restored.revisions.every((r:any)=>r.pending)).toBe(true);expect(restored.deletions).toEqual(previous.deletions);expect(restored.device).not.toBe(previous.device);
    await page.getByRole('button',{name:'Open Private records',exact:true}).click();await page.getByRole('button',{name:'Open renamed.txt',exact:true}).click();await expect(page.locator('dialog pre')).toHaveText('offline bytes never need another device');
});

for(const damage of ['authentication','truncated'] as const)test('local '+damage+' failure revokes offline readiness and sync repairs the file',async({page})=>{
    const account=await fixture(page),keys=c.unlock(account,password);
    try{
        expect((await page.request.post('/v1/signup',{data:{...account,credential:c.base64(keys.credential)}})).ok()).toBe(true);
        await openSettings(page,'Sync & devices');await page.getByRole('main').getByRole('button',{name:'Resume sync',exact:true}).click();await expect(page.locator('#status')).toContainText('Synced.');
        await page.getByRole('button',{name:'Pause sync',exact:true}).click();await page.getByRole('button',{name:'Lock',exact:true}).click();
        await page.evaluate(async({id,damage})=>{const db=await new Promise<IDBDatabase>(resolve=>{const q=indexedDB.open('klefki-v1-'+id);q.onsuccess=()=>resolve(q.result);});await new Promise<void>((resolve,reject)=>{const tx=db.transaction('chunks','readwrite'),q=tx.objectStore('chunks').openCursor();q.onsuccess=()=>{const row=q.result;if(row){const bytes=new Uint8Array(row.value);bytes[bytes.length-1]^=1;row.update(damage==='truncated'?bytes.subarray(0,bytes.length-1):bytes);}};tx.oncomplete=()=>resolve();tx.onabort=()=>reject(tx.error);});db.close();},{id:account.account,damage});
        await unlock(page);await page.getByRole('button',{name:'Open offline-secret.txt',exact:true}).click();await expect(page.locator('article')).toContainText('Not stored on this device');await expect(page.locator('dialog pre')).toHaveCount(0);
        await openSettings(page,'Sync & devices');await page.getByRole('main').getByRole('button',{name:'Resume sync',exact:true}).click();await expect(page.locator('#status')).toContainText('Synced.');
        await page.getByRole('button',{name:'Back to library',exact:true}).click();await page.getByRole('button',{name:'Open offline-secret.txt',exact:true}).click();await expect(page.locator('dialog pre')).toHaveText('offline bytes never need another device');
    }finally{c.wipe(keys.master);c.wipe(keys.credential);}
});

test('browser quota failures never report an unsaved document as saved',async({page})=>{
    await fixture(page);
    await page.evaluate(()=>{const original=IDBObjectStore.prototype.add;IDBObjectStore.prototype.add=function(...args){if(this.name==='chunks')throw new DOMException('Browser quota exhausted','QuotaExceededError');return original.apply(this,args);};});
    await upload(page,{name:'quota.txt',mimeType:'text/plain',buffer:Buffer.from('cannot save')});
    await expect(page.locator('#status')).toContainText('This change was not saved.');await expect(page.getByRole('button',{name:'Open quota.txt',exact:true})).toHaveCount(0);
    await page.getByRole('button',{name:'Open offline-secret.txt',exact:true}).click();await expect(page.locator('dialog pre')).toHaveText('offline bytes never need another device');
});
