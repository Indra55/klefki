import { openSettings, syncNow as syncUI, upload } from './ui.ts';
import {test,expect,type Page} from '@playwright/test';
import * as c from '../src/crypto.ts';

const password='public temporary relay fixture password';
async function open(page:Page,account:c.Account){
    await page.goto('/');
    await page.getByLabel('Open an encrypted account file').setInputFiles({name:'account.klefki',mimeType:'application/octet-stream',buffer:Buffer.from(c.accountBytes(account))});
    await page.getByLabel('Password',{exact:true}).fill(password);
    await page.getByRole('button',{name:'Unlock Klefki',exact:true}).click();
    await expect(page.getByRole('heading',{name:'Documents',exact:true})).toBeVisible();
}
async function syncNow(page:Page){await syncUI(page);}
async function metadataState(page:Page,account:c.Account,credential:Uint8Array){
    return page.evaluate(async({id,credential})=>{
        const login=await fetch('/v1/login',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({account:id,credential})});
        const {token}=await login.json();
        const headers={Authorization:'Bearer '+token,'X-Klefki-Content':'1'};
        const settings=await (await fetch('/v1/sync/settings',{headers})).json();
        return settings as {mode:string;queued:number};
    },{id:account.account,credential:c.base64(credential)});
}

test('files selected during sync are saved locally and survive reopening',async({page})=>{
    await c.ready;
    const fixture=c.createAccount(password);
    let resume!:()=>void;
    const paused=new Promise<void>(resolve=>{resume=resolve});
    let started!:()=>void;
    const syncing=new Promise<void>(resolve=>{started=resolve});
    try {
        await open(page,fixture.account);
        await openSettings(page,'Sync & devices');
        await page.getByRole('button',{name:'Register this store on server',exact:true}).click();
        await expect(page.getByRole('status')).toContainText('Synced.');
        await page.route('**/v1/sync/check',async route=>{started();await paused;await route.continue();});
        await syncNow(page);
        await syncing;
        await page.getByLabel('Add document').setInputFiles([
            {name:'during-sync.txt',mimeType:'text/plain',buffer:Buffer.from('retained locally')},
            {name:'during-sync.png',mimeType:'image/png',buffer:Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jWZkAAAAASUVORK5CYII=','base64')},
        ]);
        await expect(page.getByText('during-sync.txt',{exact:true})).toBeVisible();
        await expect(page.getByText('during-sync.png',{exact:true})).toBeVisible();
        resume();
        await page.evaluate(async()=>{await navigator.serviceWorker.ready;});
        await expect.poll(()=>page.evaluate(()=>!!navigator.serviceWorker.controller)).toBe(true);
        await page.context().setOffline(true);
        await page.reload();
        await page.getByLabel('Password',{exact:true}).fill(password);
        await page.getByRole('button',{name:'Unlock Klefki',exact:true}).click();
        await expect(page.getByText('during-sync.txt',{exact:true})).toBeVisible();
        await expect(page.getByText('during-sync.png',{exact:true})).toBeVisible();
        await page.getByRole('button',{name:'Open during-sync.txt',exact:true}).click();
        await expect(page.locator('dialog pre')).toHaveText('retained locally');
    } finally { resume?.();c.wipe(fixture.master); }
});

test('relay deletes delivered content, preserves offline edits, and bootstraps a late browser',async({browser,page})=>{
    await c.ready;const fixture=c.createAccount(password),account=fixture.account,keys=c.unlock(account,password);
    const second=await browser.newContext(),third=await browser.newContext();
    const b=await second.newPage(),late=await third.newPage();
    try{
        await open(page,account);
        await upload(page, {name:'relay.txt',mimeType:'text/plain',buffer:Buffer.from('private relay browser bytes')});
        await expect(page.getByText('relay.txt',{exact:true})).toBeVisible();
        await openSettings(page, 'Sync & devices');
        await page.getByRole('button',{name:'Register this store on server',exact:true}).click();
        await expect(page.getByRole('status')).toContainText('Synced.');
        await expect(page.getByRole('button',{name:'Sync status',exact:true})).not.toHaveAttribute('data-state','running');
        await openSettings(page, 'Sync & devices');
        await page.getByLabel('Sync mode',{exact:true}).selectOption('temporary-relay');
        await page.getByRole('button',{name:'Apply sync mode'}).click();
        await expect(page.getByRole('status')).toContainText('Sync mode updated');
        expect((await metadataState(page,account,keys.credential)).mode).toBe('temporary-relay');
        expect((await metadataState(page,account,keys.credential)).queued).toBe(1);
        await open(b,account);await syncNow(b);
        await expect(b.getByRole('status')).toContainText('removed from the relay');
        await expect(b.getByRole('button',{name:'Open relay.txt',exact:true})).toBeVisible();
        expect((await metadataState(page,account,keys.credential)).queued).toBe(0);
        // Purged server history must not be mistaken for an empty library.
        await open(late,account);await syncNow(late);
        await syncNow(page);
        await syncNow(late);await expect(late.getByRole('button',{name:'Open relay.txt',exact:true})).toBeVisible();
        await expect(late.locator('article').filter({has:late.getByText('relay.txt',{exact:true})})).not.toContainText('Not stored on this device');
        await late.getByRole('button',{name:'Open relay.txt',exact:true}).click();
        await expect(late.locator('dialog pre')).toHaveText('private relay browser bytes');
        await late.getByRole('button',{name:'Close',exact:true}).click();
        await page.evaluate(async()=>{await navigator.serviceWorker.ready;});
        await expect.poll(()=>page.evaluate(()=>!!navigator.serviceWorker.controller)).toBe(true);
        await page.context().setOffline(true);
        await upload(page, {name:'offline-relay.txt',mimeType:'text/plain',buffer:Buffer.from('offline relay edit')});
        await expect(page.getByText('offline-relay.txt',{exact:true})).toBeVisible();
        await page.reload();await page.getByLabel('Password',{exact:true}).fill(password);await page.getByRole('button',{name:'Unlock Klefki',exact:true}).click();
        await expect(page.getByText('offline-relay.txt',{exact:true})).toBeVisible();
        await page.context().setOffline(false);await syncNow(page);
        await expect(page.getByRole('status')).toContainText('Waiting for linked devices');
        await syncNow(b);await expect(b.getByRole('button',{name:'Open offline-relay.txt',exact:true})).toBeVisible();
        await syncNow(late);await expect(late.getByRole('button',{name:'Open offline-relay.txt',exact:true})).toBeVisible();
        await expect(b.locator('article').filter({has:b.getByText('offline-relay.txt',{exact:true})})).not.toContainText('Not stored on this device');
        await expect(late.locator('article').filter({has:late.getByText('offline-relay.txt',{exact:true})})).not.toContainText('Not stored on this device');
        await expect.poll(async()=>(await metadataState(page,account,keys.credential)).queued).toBe(0);
    }finally{await second.close();await third.close();c.wipe(fixture.master);c.wipe(keys.master);c.wipe(keys.credential);}
});

test('84 retained revisions use bounded metadata requests and require compatible preparation',async({page})=>{
    await c.ready;const fixture=c.createAccount(password);
    try {
        await open(page,fixture.account);
        await openSettings(page,'Sync & devices');
        await page.getByRole('button',{name:'Register this store on server',exact:true}).click();
        await expect(page.getByRole('status')).toContainText('Synced.');
        await expect(page.getByRole('button',{name:'Sync status',exact:true})).not.toHaveAttribute('data-state','running');
        const revisions=Array.from({length:84},(_,i)=>{const object=c.id();return {revision:{id:c.id(),object,base:'',metadata:c.base64(c.encryptMetadata(fixture.master,fixture.account.account,object,{type:'folder',name:'batch-'+i,parent:null,tags:[],favorite:false}))},order:i+1,pending:true,metadataPending:true,metadataQueued:1};});
        await page.evaluate(async({account,revisions})=>{
            const db=await new Promise<IDBDatabase>((resolve,reject)=>{const r=indexedDB.open('klefki-v1-'+account);r.onsuccess=()=>resolve(r.result);r.onerror=()=>reject(r.error);});
            await new Promise<void>((resolve,reject)=>{const tx=db.transaction(['revisions','state'],'readwrite');for(const r of revisions)tx.objectStore('revisions').put(r);tx.objectStore('state').put(84,'order');tx.oncomplete=()=>resolve();tx.onerror=()=>reject(tx.error);});db.close();
        },{account:fixture.account.account,revisions});
        await syncNow(page);
        await expect(page.getByRole('button',{name:'Sync status',exact:true})).not.toHaveAttribute('data-state','running');
        await page.keyboard.press('Escape');
        for(const legacy of [0,404,405]){
            const calls:string[]=[];
            await page.route('**/v1/**',async route=>{
                const path=new URL(route.request().url()).pathname;
                if(['/v1/sync/relay/prepare','/v1/sync/relay/device','/v1/sync/relay/need','/v1/sync/relay/need-batch','/v1/sync/changes','/v1/sync/cursor'].includes(path)||path.includes('/chunks/'))calls.push(path);
                if(legacy&&path==='/v1/sync/settings'){
                    const response=await route.fetch();const settings=await response.json();delete settings.work_queue;
                    await route.fulfill({response,json:settings});
                }else if(legacy&&path==='/v1/sync/relay/prepare')await route.fulfill({status:legacy,body:''});else await route.continue();
            });
            await syncNow(page);
            await expect(page.getByRole('button',{name:'Sync status',exact:true})).not.toHaveAttribute('data-state','running');
            expect(calls).toHaveLength(legacy?1:0);
            if(legacy)await expect(page.getByRole('status')).toContainText('Could not complete sync');
            expect(calls.some(p=>p.includes('/chunks/'))).toBe(false);
            await page.unroute('**/v1/**');await page.keyboard.press('Escape');
        }
    } finally {c.wipe(fixture.master);}
});
