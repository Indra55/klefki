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
        const headers={Authorization:'Bearer '+token};
        const settings=await (await fetch('/v1/sync/settings',{headers})).json();
        return settings as {mode:string;queued:number};
    },{id:account.account,credential:c.base64(credential)});
}

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
        await openSettings(page, 'Sync & devices');
        await page.getByLabel('Sync mode',{exact:true}).selectOption('temporary-relay');
    await page.getByRole('button',{name:'Apply sync mode'}).click();
        await expect(page.getByRole('status')).toContainText('Sync mode updated');
        expect((await metadataState(page,account,keys.credential)).mode).toBe('temporary-relay');
        expect((await metadataState(page,account,keys.credential)).queued).toBe(1);
        await open(b,account);await syncNow(b);
        await expect(b.getByRole('status')).toContainText('removed from the relay');
        await expect(b.getByText('relay.txt',{exact:true})).toBeVisible();
        expect((await metadataState(page,account,keys.credential)).queued).toBe(0);
        // Purged server history must not be mistaken for an empty library.
        await open(late,account);await syncNow(late);
        await expect(late.getByRole('status')).toContainText('another linked device');
        await syncNow(page);await expect(page.getByRole('status')).toContainText('Waiting for linked devices');
        await syncNow(late);await expect(late.getByText('relay.txt',{exact:true})).toBeVisible();
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
        await syncNow(b);await expect(b.getByText('offline-relay.txt',{exact:true})).toBeVisible();
        await syncNow(late);await expect(late.getByText('offline-relay.txt',{exact:true})).toBeVisible();
        expect((await metadataState(page,account,keys.credential)).queued).toBe(0);
    }finally{await second.close();await third.close();c.wipe(fixture.master);c.wipe(keys.master);c.wipe(keys.credential);}
});
