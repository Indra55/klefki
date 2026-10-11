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
async function connect(page: Page) { await page.goto('/'); await page.getByLabel('Open an encrypted account file').setInputFiles(join(dir, 'account.klefki')); await unlock(page); await page.getByRole('button', { name: 'Sync now' }).click(); await expect(page.getByRole('status')).toContainText('Synced.'); }
const row = (page: Page, name: string) => page.locator('article').filter({ has: page.getByText(name, { exact: true }) });
async function editText(page: Page, text: string) { await row(page, 'note.txt').getByRole('button', { name: 'Edit', exact: true }).click(); await page.getByRole('button', { name: 'Edit text content' }).click(); await page.getByLabel('Document text').fill(text); await page.getByRole('button', { name: 'Save changes' }).click(); await expect(page.getByRole('status')).toContainText('Changes saved locally.'); }
async function durable(page: Page) { await page.evaluate(async () => { await navigator.serviceWorker.ready; }); await expect.poll(() => page.evaluate(() => !!navigator.serviceWorker.controller)).toBe(true); }
test('desktop to web, web to desktop, offline reload and content edit', async ({ page, context }) => {
    await connect(page);
    await durable(page);
    const download = page.waitForEvent('download');
    await row(page, 'desktop.bin').getByRole('button', { name: 'Download', exact: true }).click();
    const b = readFileSync((await (await download).path())!);
    expect(b.length).toBe(4194304 + 19);
    expect(b.every((x, i) => x === i % 251)).toBe(true);
    await page.getByLabel('Add document').setInputFiles({ name: 'web.txt', mimeType: 'text/plain', buffer: Buffer.from('uploaded from web') });
    await expect(row(page, 'web.txt')).toBeVisible();
    await context.setOffline(true);
    await editText(page, 'offline web text');
    await page.reload();
    await unlock(page);
    await expect(row(page, 'web.txt')).toBeVisible();
    await row(page, 'note.txt').getByRole('button', { name: 'Open', exact: true }).click();
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
    await page.getByText('Klefki settings',{exact:true}).click();
    await page.getByLabel('Temporary relay',{exact:true}).check();
    await expect(page.getByRole('status')).toContainText('Sync mode updated');
    await page.getByLabel('Add document').setInputFiles({name:'web.txt',mimeType:'text/plain',buffer:Buffer.from('uploaded from web')});
    await expect(row(page,'web.txt')).toBeVisible();
    await editText(page,'offline web text');
    await page.getByRole('button',{name:'Sync now',exact:true}).click();
    await expect(page.getByRole('status')).toContainText('Waiting for linked devices');
    expect(bridge('verify')).toContain('PASS verify');
    await page.getByRole('button',{name:'Sync now',exact:true}).click();
    await expect(page.getByRole('status')).toContainText('removed from the relay');
    const download=page.waitForEvent('download');
    await row(page,'desktop.bin').getByRole('button',{name:'Download',exact:true}).click();
    const data=readFileSync((await(await download).path())!);
    expect(data.length).toBe(4194304+19);expect(data.every((x,i)=>x===i%251)).toBe(true);
});
test('interrupted uploads retain pending work and retry stored ciphertext', async ({ page }) => {
    await connect(page);
    await page.getByLabel('Add document').setInputFiles({ name: 'retry.txt', mimeType: 'text/plain', buffer: Buffer.from('durable queued upload') });
    await expect(row(page, 'retry.txt')).toBeVisible();
    await page.route('**/v1/sync/revisions/*', route => route.abort());
    await page.getByRole('button', { name: 'Sync now' }).click();
    await expect(page.getByRole('status')).toContainText('Could not complete');
    await page.unroute('**/v1/sync/revisions/*');
    await page.reload();
    await unlock(page);
    await expect(row(page, 'retry.txt')).toBeVisible();
    await page.getByRole('button', { name: 'Sync now' }).click();
    await expect(page.getByRole('status')).toContainText('Synced.');
});
test('tampered download is rejected without committing the revision or cursor', async ({ page }) => {
    await page.goto('/');
    await page.getByLabel('Open an encrypted account file').setInputFiles(join(dir, 'account.klefki'));
    await unlock(page);
    await page.route('**/v1/blobs/*/chunks/*', async (route) => { const response = await route.fetch(); const b = await response.body(); b[b.length - 1] ^= 1; await route.fulfill({ response, body: b }); });
    await page.getByRole('button', { name: 'Sync now' }).click();
    await expect(page.getByRole('status')).toContainText('Could not complete');
    await expect(page.locator('article')).toHaveCount(0);
    const cursor = await page.evaluate(async () => { const a = JSON.parse(localStorage.getItem('klefki-account')!); return new Promise<number>(resolve => { const r = indexedDB.open('klefki-v1-' + a.account); r.onsuccess = () => { const db = r.result, q = db.transaction('state').objectStore('state').get('cursor'); q.onsuccess = () => { db.close(); resolve(q.result || 0); }; }; }); });
    expect(cursor).toBe(0);
    await page.unroute('**/v1/blobs/*/chunks/*');
    await page.getByRole('button', { name: 'Sync now' }).click();
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
 await page.getByText('Klefki settings',{exact:true}).click();
 await page.getByRole('button',{name:'Register this store on server'}).click();
 await expect(page.getByRole('status')).toContainText('Synced.');
 await page.getByLabel('Add document').setInputFiles({name:'web.txt',mimeType:'text/plain',buffer:Buffer.from('uploaded from web')});
 await expect(row(page,'web.txt')).toBeVisible();
 await row(page,'web.txt').getByRole('button',{name:'Edit',exact:true}).click();
 await page.getByLabel('Name',{exact:true}).fill('renamed.txt');
 await page.getByLabel('Favorite',{exact:true}).check();
 await page.getByRole('button',{name:'Save changes'}).click();
 await expect(row(page,'renamed.txt')).toBeVisible();
 await page.getByRole('button',{name:'Sync now'}).click();
 await expect(page.getByRole('status')).toContainText('Synced.');
 await page.clock.fastForward(301000);
 await expect(page.getByRole('button',{name:'Unlock Klefki'})).toBeVisible();
 await expect(page.locator('article')).toHaveCount(0);
});
