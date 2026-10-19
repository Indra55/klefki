import { openSettings, syncNow as syncUI, documentAction, upload } from './ui.ts';
import { test, expect, type Page } from '@playwright/test';
import { execFileSync } from 'node:child_process';
import { mkdtempSync, rmSync, readFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
const origin = 'http://127.0.0.1:5188', password = 'public M5 browser fixture password';
let dir: string;
const bridge = (mode: string) => execFileSync('go', ['run', './tests/bridge', mode, dir, origin], { stdio: 'pipe', timeout: 90000 }).toString();
test.beforeEach(() => { dir = mkdtempSync(join(tmpdir(), 'klefki-m5-fixture-')); bridge('seed'); });
test.afterEach(() => rmSync(dir, { recursive: true, force: true }));
async function unlock(page: Page) { await page.getByLabel('Password', { exact: true }).fill(password); await page.getByRole('button', { name: 'Unlock Klefki', exact: true }).click(); await expect(page.getByRole('heading', { name: 'Documents', exact: true })).toBeVisible(); }
async function connect(page: Page) { await page.goto('/'); await page.getByLabel('Open an encrypted account file').setInputFiles(join(dir, 'account.klefki')); await unlock(page); await syncUI(page); await expect(page.getByRole('status')).toContainText('Synced.'); }
const row = (page: Page, name: string) => page.locator('article').filter({ has: page.getByText(name, { exact: true }) });
async function editText(page: Page, text: string) { await documentAction(page, 'note.txt', 'Edit details & move'); await page.getByRole('button', { name: 'Edit text content' }).click(); await page.getByLabel('Document text').fill(text); await page.getByRole('button', { name: 'Save changes' }).click(); await expect(page.getByRole('status')).toContainText('Changes saved locally.'); }
async function durable(page: Page) { await page.evaluate(async () => { await navigator.serviceWorker.ready; }); await expect.poll(() => page.evaluate(() => !!navigator.serviceWorker.controller)).toBe(true); }
test('desktop to web, web to desktop, offline reload and content edit', async ({ page, context }) => {
    await connect(page);
    await durable(page);
    const download = page.waitForEvent('download');
    await documentAction(page, 'desktop.bin', 'Export');
    const b = readFileSync((await (await download).path())!);
    expect(b.length).toBe(4194304 + 19);
    expect(b.every((x, i) => x === i % 251)).toBe(true);
    await upload(page, { name: 'web.txt', mimeType: 'text/plain', buffer: Buffer.from('uploaded from web') });
    await expect(row(page, 'web.txt')).toBeVisible();
    await context.setOffline(true);
    await editText(page, 'offline web text');
    await page.reload();
    await unlock(page);
    await expect(row(page, 'web.txt')).toBeVisible();
    await row(page, 'note.txt').getByRole('button', { name: 'Open note.txt', exact: true }).click();
    await expect(page.locator('dialog pre')).toHaveText('offline web text');
    await page.getByRole('button', { name: 'Close', exact: true }).click();
    const persisted = await page.evaluate(async () => { const a = JSON.parse(localStorage.getItem('klefki-account')!); const db = await new Promise<IDBDatabase>((resolve, reject) => { const r = indexedDB.open('klefki-v1-' + a.account); r.onsuccess = () => resolve(r.result); r.onerror = reject; }); const tx = db.transaction(['state', 'revisions', 'chunks']); const all = await Promise.all([...tx.objectStoreNames].map(n => new Promise(resolve => { const r = tx.objectStore(n).getAll(); r.onsuccess = () => resolve(r.result); }))); db.close(); return JSON.stringify(all); });
    for (const secret of ['web.txt', 'uploaded from web', 'offline web text', password, 'note.txt'])
        expect(persisted).not.toContain(secret);
    await context.setOffline(false);
    await expect(page.getByRole('status')).toContainText('Synced.');
    expect(bridge('verify')).toContain('PASS verify');
    await page.getByRole('button', { name: 'Lock', exact: true }).click();
    await expect(page.getByRole('button', { name: 'Unlock Klefki' })).toBeVisible();
    await expect(page.getByText('offline web text')).toHaveCount(0);
});
test('offline desktop and web edits preserve both conflict copies', async ({ page, context }) => {
    await connect(page);
    await context.setOffline(true);
    await editText(page, 'offline web text');
    expect(bridge('desktop-edit')).toContain('PASS');
    await context.setOffline(false);
    await expect(page.getByRole('status')).toContainText('Synced.');
    await expect(row(page, 'note.txt')).toHaveCount(2);
    await expect(page.getByText(/Conflict copy/)).toHaveCount(2);
    expect(bridge('verify-conflict')).toContain('PASS');
});
test('temporary relay transfers Go and browser documents after server cleanup',async({page})=>{
    await connect(page);
    await openSettings(page, 'Sync & devices');
    await page.getByLabel('Sync mode',{exact:true}).selectOption('temporary-relay');
    await page.getByRole('button',{name:'Apply sync mode'}).click();
    await expect(page.getByRole('status')).toContainText('Sync mode updated');
    await upload(page, {name:'web.txt',mimeType:'text/plain',buffer:Buffer.from('uploaded from web')});
    await expect(row(page,'web.txt')).toBeVisible();
    await editText(page,'offline web text');
    await syncUI(page);
    await expect(page.getByRole('status')).toContainText('Waiting for linked devices');
    expect(bridge('verify')).toContain('PASS verify');
    await syncUI(page);
    await expect(page.getByRole('status')).toContainText('removed from the relay');
    const download=page.waitForEvent('download');
    await documentAction(page, 'desktop.bin', 'Export');
    const data=readFileSync((await(await download).path())!);
    expect(data.length).toBe(4194304+19);expect(data.every((x,i)=>x===i%251)).toBe(true);
});
test('interrupted uploads retain pending work and retry stored ciphertext', async ({ page }) => {
    await connect(page);
    await upload(page, { name: 'retry.txt', mimeType: 'text/plain', buffer: Buffer.from('durable queued upload') });
    await expect(row(page, 'retry.txt')).toBeVisible();
    await page.route('**/v1/sync/revisions/*', route => route.abort());
    await syncUI(page);
    await expect(page.getByRole('status')).toContainText('Could not complete');
    await page.unroute('**/v1/sync/revisions/*');
    await page.reload();
    await unlock(page);
    await expect(row(page, 'retry.txt')).toBeVisible();
    await syncUI(page);
    await expect(page.getByRole('status')).toContainText('Synced.');
});
test('tampered contents are rejected while authenticated metadata stays visible without a delivery receipt', async ({ page }) => {
    await page.goto('/');
    await page.getByLabel('Open an encrypted account file').setInputFiles(join(dir, 'account.klefki'));
    await unlock(page);
    await page.route('**/v1/blobs/*/chunks/*', async (route) => { const response = await route.fetch(); const b = await response.body(); b[b.length - 1] ^= 1; await route.fulfill({ response, body: b }); });
    await syncUI(page);
    await expect(page.getByRole('status')).toContainText('Could not complete');
    await expect(page.locator('article')).toHaveCount(3);
    await expect(page.getByText('Not stored on this device',{exact:true})).toHaveCount(3);
    const local = await page.evaluate(async () => {
        const a = JSON.parse(localStorage.getItem('klefki-account')!);
        const db = await new Promise<IDBDatabase>((resolve,reject) => { const r=indexedDB.open('klefki-v1-'+a.account);r.onsuccess=()=>resolve(r.result);r.onerror=()=>reject(r.error); });
        const tx=db.transaction(['state','chunks']);
        const cursor=new Promise<number>(resolve=>{const q=tx.objectStore('state').get('cursor');q.onsuccess=()=>resolve(q.result||0);});
        const chunks=new Promise<number>(resolve=>{const q=tx.objectStore('chunks').count();q.onsuccess=()=>resolve(q.result);});
        const result={cursor:await cursor,chunks:await chunks};db.close();return result;
    });
    expect(local).toEqual({cursor:0,chunks:0});
    await page.unroute('**/v1/blobs/*/chunks/*');
    await syncUI(page);
    await expect(page.getByRole('status')).toContainText('Synced.');
    await expect(page.locator('article')).toHaveCount(3);
});
test('another tab cannot open the same store until the first locks', async ({ page, context }) => {
    await connect(page);
    const other = await context.newPage();
    await other.goto('/');
    await other.getByLabel('Password', { exact: true }).fill(password);
    await other.getByRole('button', { name: 'Unlock Klefki' }).click();
    await expect(other.getByRole('status')).toContainText('close other tabs');
    await page.getByRole('button', { name: 'Lock', exact: true }).click();
    await unlock(other);
    await expect(other.locator('article')).toHaveCount(3);
});

test('web account creation, registration, metadata edit and automatic lock', async ({page}) => {
 await page.clock.install();
 await page.goto('/');
 await page.getByLabel('Password',{exact:true}).fill(password);
 await page.getByLabel('Confirm password').fill(password);
 await page.getByRole('button',{name:'Create Klefki',exact:true}).click();
 await expect(page.getByRole('heading',{name:'Save your recovery code'})).toBeVisible();
 await expect(page.locator('dialog code')).toHaveText(/^vlt1-[A-Z2-7]{52}$/);
 await page.getByRole('button',{name:'I have saved it'}).click();
 await openSettings(page, 'Sync & devices');
 await page.getByRole('button',{name:'Register this store on server'}).click();
 await expect(page.getByRole('status')).toContainText('Synced.');
 await upload(page, {name:'web.txt',mimeType:'text/plain',buffer:Buffer.from('uploaded from web')});
 await expect(row(page,'web.txt')).toBeVisible();
 await documentAction(page, 'web.txt', 'Edit details & move');
 await page.getByLabel('Name',{exact:true}).fill('renamed.txt');
 await page.getByLabel('Favorite',{exact:true}).check();
 await page.getByRole('button',{name:'Save changes'}).click();
 await expect(row(page,'renamed.txt')).toBeVisible();
 await syncUI(page);
 await expect(page.getByRole('status')).toContainText('Synced.');
 await page.clock.fastForward(301000);
 await expect(page.getByRole('button',{name:'Unlock Klefki'})).toBeVisible();
 await expect(page.locator('article')).toHaveCount(0);
});

async function trashView(page:Page){await page.getByRole('button',{name:'View',exact:true}).click();await page.getByRole('menuitem',{name:'Trash',exact:true}).click();}
test('permanent trash deletion syncs to Go and survives offline reload',async({page,context})=>{
 await connect(page);await durable(page);
 await documentAction(page,'note.txt','Move to Trash');await trashView(page);
 await expect(row(page,'note.txt')).toContainText('Deletes');
 await context.setOffline(true);
 await documentAction(page,'note.txt','Delete permanently');
 const confirmation=page.getByRole('dialog',{name:'Delete permanently?'});
 await expect(confirmation).toContainText('historical versions and conflict copies');
 await confirmation.getByRole('button',{name:'Cancel',exact:true}).click();await expect(row(page,'note.txt')).toBeVisible();
 await documentAction(page,'note.txt','Delete permanently');await page.getByRole('dialog',{name:'Delete permanently?'}).getByRole('button',{name:'Delete permanently',exact:true}).click();
 await expect(row(page,'note.txt')).toHaveCount(0);
 await page.reload();await unlock(page);await trashView(page);await expect(row(page,'note.txt')).toHaveCount(0);
 const local=await page.evaluate(async()=>{const a=JSON.parse(localStorage.getItem('klefki-account')!);const db=await new Promise<IDBDatabase>(resolve=>{const q=indexedDB.open('klefki-v1-'+a.account);q.onsuccess=()=>resolve(q.result);});const ledger=await new Promise<any[]>(resolve=>{const q=db.transaction('deletions').objectStore('deletions').getAll();q.onsuccess=()=>resolve(q.result);});db.close();return ledger;});
 expect(local.length).toBeGreaterThan(0);expect(local.some(d=>d.pending)).toBe(true);
 await context.setOffline(false);await syncUI(page);await expect(page.getByRole('status')).toContainText('Synced.');
 expect(bridge('verify-deletion')).toContain('PASS verify-deletion');
});
test('Empty Trash ignores the search filter and preserves live documents',async({page})=>{
 await connect(page);
 for(const name of ['note.txt','empty.txt'])await documentAction(page,name,'Move to Trash');
 await trashView(page);await page.getByRole('searchbox',{name:'Search names and tags'}).fill('note');
 await page.getByRole('button',{name:'Empty Trash',exact:true}).click();
 const dialog=page.getByRole('dialog',{name:'Empty Trash?'});await expect(dialog).toContainText('2 documents and folders');
 await dialog.getByRole('button',{name:'Delete permanently',exact:true}).click();
 await expect(page.getByRole('dialog',{name:'Empty Trash?'})).toHaveCount(0);
 await expect(page.getByText('Nothing matches yet.',{exact:true})).toBeVisible();
 await page.getByRole('button',{name:'Clear search',exact:true}).click();await expect(page.getByText('Your trash is empty.',{exact:true})).toBeVisible();
 await page.getByRole('button',{name:'View',exact:true}).click();await page.getByRole('menuitem',{name:'All documents',exact:true}).click();await expect(row(page,'desktop.bin')).toBeVisible();
});
test('browser applies an automatic expiry performed by Go without restoring history',async({page})=>{
 await connect(page);expect(bridge('expire-trash')).toContain('PASS expire-trash');
 await syncUI(page);await expect(page.getByRole('status')).toContainText('Synced.');
 await expect(row(page,'note.txt')).toHaveCount(0);await trashView(page);await expect(row(page,'note.txt')).toHaveCount(0);
 expect(bridge('verify-deletion')).toContain('PASS verify-deletion');
});

test('browser automatically cleans an expired encrypted trash revision after sync',async({page,context})=>{
 await connect(page);await documentAction(page,'note.txt','Move to Trash');await trashView(page);
 await context.setOffline(true);
 const stored=await page.evaluate(async()=>{const account=JSON.parse(localStorage.getItem('klefki-account')!);const db=await new Promise<IDBDatabase>(resolve=>{const q=indexedDB.open('klefki-v1-'+account.account);q.onsuccess=()=>resolve(q.result);});const all=await new Promise<any[]>(resolve=>{const q=db.transaction('revisions').objectStore('revisions').getAll();q.onsuccess=()=>resolve(q.result);});db.close();return {account,all};});
 const crypto=await import('../src/crypto.ts');await crypto.ready;const keys=crypto.unlock(stored.account,password);
 try{
  const parents=new Set(stored.all.map(x=>x.revision.base));const selected=stored.all.find(x=>!parents.has(x.revision.id)&&crypto.decryptMetadata(keys.master,stored.account.account,x.revision.object,crypto.unbase64(x.revision.metadata)).name==='note.txt');
  const metadata=crypto.decryptMetadata(keys.master,stored.account.account,selected.revision.object,crypto.unbase64(selected.revision.metadata));metadata.trashed_at=Date.now()-31*86400000;
  const revision={...selected.revision,id:crypto.id(),base:selected.revision.id,metadata:crypto.base64(crypto.encryptMetadata(keys.master,stored.account.account,selected.revision.object,metadata))};
  await page.evaluate(async({account,revision})=>{const db=await new Promise<IDBDatabase>(resolve=>{const q=indexedDB.open('klefki-v1-'+account);q.onsuccess=()=>resolve(q.result);});await new Promise<void>((resolve,reject)=>{const tx=db.transaction(['state','revisions'],'readwrite'),state=tx.objectStore('state'),q=state.get('order');q.onsuccess=()=>{const order=(q.result||0)+1;state.put(order,'order');tx.objectStore('revisions').put({revision,pending:true,order});};tx.oncomplete=()=>resolve();tx.onabort=()=>reject(tx.error);});db.close();},{account:stored.account.account,revision});
  await context.setOffline(false);await syncUI(page);await expect(page.getByRole('status')).toContainText('Synced.');await expect(row(page,'note.txt')).toHaveCount(0);
  expect(bridge('verify-deletion')).toContain('PASS verify-deletion');
 }finally{crypto.wipe(keys.master);crypto.wipe(keys.credential);}
});
