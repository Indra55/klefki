import {test,expect,type Page} from '@playwright/test';
import * as c from '../src/crypto.ts';
import {backToLibrary,openSettings,upload} from './ui.ts';

const password='public device status fixture password';
async function open(page:Page,account:c.Account) {
 await page.goto('/');
 await page.getByLabel('Open an encrypted account file').setInputFiles({name:'account.klefki',mimeType:'application/octet-stream',buffer:Buffer.from(c.accountBytes(account))});
 await page.getByLabel('Password',{exact:true}).fill(password);
 await page.getByRole('button',{name:'Unlock Klefki',exact:true}).click();
 await expect(page.getByRole('heading',{name:'Documents',exact:true})).toBeVisible();
}
async function register(page:Page) {
 await openSettings(page,'Sync & devices');
 await page.getByRole('button',{name:'Register this store on server',exact:true}).click();
 await expect(page.getByRole('status')).toContainText('Synced.');
 await page.getByLabel('Sync mode',{exact:true}).selectOption('temporary-relay');
 await page.getByRole('button',{name:'Apply sync mode',exact:true}).click();
 await expect(page.getByRole('status')).toContainText('Sync mode updated');
 await backToLibrary(page);
}
async function reports(page:Page,account:c.Account,credential:Uint8Array) {
 return page.evaluate(async({account,credential})=>{
  const login=await fetch('/v1/login',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({account,credential})});
  const {token}=await login.json();
  return (await fetch('/v1/sync/status?deviceState=1',{headers:{Authorization:'Bearer '+token,'X-Klefki-Deletion':'1'}})).json();
 },{account:account.account,credential:c.base64(credential)}) as Promise<{devices:{device:string;outstanding:number;deviceState?:{pending:boolean;version:number}}[]}>;
}

test('relay delivers after receiver closes and source closes before receipt',async({page,browser})=>{
 await c.ready;const fixture=c.createAccount(password),keys=c.unlock(fixture.account,password);
 const receiver=await browser.newContext();let b=await receiver.newPage();
 try {
  await open(page,fixture.account);await register(page);
  await open(b,fixture.account);
  await expect.poll(async()=>(await reports(page,fixture.account,keys.credential)).devices.length).toBe(2);
  await b.close();
  await upload(page,{name:'closed-receiver.txt',mimeType:'text/plain',buffer:Buffer.from('stored without overlapping device uptime')});
  await expect.poll(async()=>(await reports(page,fixture.account,keys.credential)).devices.some(d=>d.outstanding>0)).toBe(true);
  await expect.poll(async()=>(await reports(page,fixture.account,keys.credential)).devices.some(d=>d.deviceState?.pending)).toBe(false);
  await expect(page.getByRole('button',{name:'Sync status',exact:true})).toHaveAttribute('data-state','waiting');
  await page.close();
  b=await receiver.newPage();await b.goto('/');
  await b.getByLabel('Password',{exact:true}).fill(password);await b.getByRole('button',{name:'Unlock Klefki',exact:true}).click();
  await expect(b.getByRole('button',{name:'Open closed-receiver.txt',exact:true})).toBeVisible();
  await b.getByRole('button',{name:'Open closed-receiver.txt',exact:true}).click();
  await expect(b.locator('dialog pre')).toHaveText('stored without overlapping device uptime');
 } finally {await receiver.close();c.wipe(fixture.master);c.wipe(keys.master);c.wipe(keys.credential);}
});

test('PWA pending report survives closure, clears on upload and makes no idle writes',async({page,browser})=>{
 await c.ready;const fixture=c.createAccount(password),keys=c.unlock(fixture.account,password);
 const source=await browser.newContext();let a=await source.newPage();
 let statePosts=0;
 try {
  await open(page,fixture.account);await register(page);
  // Exercise the PWA reporting branch; Chromium does not emulate this media feature.
  await source.addInitScript(()=>{const original=window.matchMedia.bind(window);window.matchMedia=query=>{const result=original(query);if(query==='(display-mode: standalone)')Object.defineProperty(result,'matches',{value:true});return result;};});
  await open(a,fixture.account);
  expect(await a.evaluate(()=>matchMedia('(display-mode: standalone)').matches)).toBe(true);
  await expect.poll(async()=>(await reports(page,fixture.account,keys.credential)).devices.length).toBe(2);
  a.on('request',r=>{if(new URL(r.url()).pathname==='/v1/sync/device-state')statePosts++;});
  await a.route('**/v1/blobs/**',route=>route.fulfill({status:507,body:'Upload blocked for fixture'}));
  await upload(a,{name:'waiting-source.txt',mimeType:'text/plain',buffer:Buffer.from('local source bytes')});
  await expect.poll(async()=>(await reports(page,fixture.account,keys.credential)).devices.some(d=>d.deviceState?.pending)).toBe(true);
  await a.close();
  await page.getByRole('button',{name:'Sync status',exact:true}).click();
  const panel=page.getByRole('dialog',{name:'Sync activity'});
  await expect(panel).toContainText('PWA');
  await expect(panel).toContainText('reported changes waiting to upload');
  await expect(panel).toContainText('Reopen this device to finish syncing');
  await expect(panel).toContainText('Pending-state report');
  await expect(page.getByRole('button',{name:'Sync status',exact:true})).toHaveAttribute('data-state','waiting');
  await expect(page.getByRole('button',{name:'Open waiting-source.txt',exact:true})).toHaveCount(0);
  a=await source.newPage();await a.goto('/');
  a.on('request',r=>{if(new URL(r.url()).pathname==='/v1/sync/device-state')statePosts++;});
  await a.getByLabel('Password',{exact:true}).fill(password);await a.getByRole('button',{name:'Unlock Klefki',exact:true}).click();
  await expect.poll(async()=>(await reports(page,fixture.account,keys.credential)).devices.some(d=>d.deviceState?.pending)).toBe(false);
  await expect(panel.getByText(/Reopen this device to finish syncing/)).toHaveCount(0);
  const before=statePosts;
  await a.clock.install();await a.clock.fastForward(16000);
  await expect.poll(()=>statePosts).toBe(before);
  await panel.getByRole('button',{name:'Refresh sync',exact:true}).click();
  await expect(page.getByRole('button',{name:'Open waiting-source.txt',exact:true})).toBeVisible();
  await expect.poll(async()=>(await reports(page,fixture.account,keys.credential)).devices.every(d=>d.outstanding===0)).toBe(true);
 } finally {await source.close();c.wipe(fixture.master);c.wipe(keys.master);c.wipe(keys.credential);}
});
