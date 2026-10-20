import {test,expect,type Page} from '@playwright/test';
import * as c from '../src/crypto.ts';
import {openSettings,backToLibrary,upload} from './ui.ts';
const password='public sync worker fixture password';
async function open(page:Page,account:c.Account){
 await page.goto('/');
 await page.getByLabel('Open an encrypted account file').setInputFiles({name:'account.klefki',mimeType:'application/octet-stream',buffer:Buffer.from(c.accountBytes(account))});
 await page.getByLabel('Password',{exact:true}).fill(password);await page.getByRole('button',{name:'Unlock Klefki',exact:true}).click();await expect(page.getByRole('heading',{name:'Documents',exact:true})).toBeVisible();
}
async function register(page:Page){await openSettings(page,'Sync & devices');await page.getByRole('button',{name:'Register this store on server',exact:true}).click();await expect(page.getByRole('status')).toContainText('Synced.');await backToLibrary(page);}

test('new documents arrive automatically past a failed older file, with durable unread activity',async({page,browser})=>{
 await c.ready;const fixture=c.createAccount(password),second=await browser.newContext(),receiver=await second.newPage();
 try{
  await open(page,fixture.account);await register(page);await open(receiver,fixture.account);
  await expect(receiver.getByRole('button',{name:'Sync status',exact:true})).toHaveAttribute('data-state','synced');
  let blockedFile='';let failed=0;
  await receiver.route('**/v1/blobs/*/chunks/*',async route=>{
   if(route.request().method()==='GET'){
    const file=new URL(route.request().url()).pathname.split('/')[3];blockedFile ||= file;
    if(file===blockedFile){failed++;await route.fulfill({status:503,body:''});return;}
   }
   await route.continue();
  });
  await upload(page,{name:'old-failing.txt',mimeType:'text/plain',buffer:Buffer.from('old worker fixture')});
  await expect.poll(()=>failed).toBeGreaterThan(0);
  await upload(page,{name:'scan_new.txt',mimeType:'text/plain',buffer:Buffer.from('new worker fixture')});
  await expect(receiver.getByRole('button',{name:'Open scan_new.txt',exact:true})).toBeVisible({timeout:20000});
  await expect(receiver.locator('.incoming-toast')).toContainText('scan_new.txt is ready');
  const activity=receiver.getByRole('button',{name:/Incoming documents/});await expect(activity).toContainText('1');
  await activity.click();const dialog=receiver.getByRole('dialog',{name:'Incoming documents'});await expect(dialog).toContainText('scan_new.txt');
  await receiver.evaluate(()=>{Notification.requestPermission=async()=> 'denied';});
  await dialog.getByRole('button',{name:'Enable system notifications',exact:true}).click();
  await expect(dialog.getByRole('button',{name:'System notifications unavailable',exact:true})).toBeDisabled();
  await dialog.getByRole('button',{name:'Open scan_new.txt',exact:true}).click();await expect(receiver.locator('dialog pre')).toHaveText('new worker fixture');
  await receiver.keyboard.press('Escape');
  await receiver.reload();await receiver.getByLabel('Password',{exact:true}).fill(password);await receiver.getByRole('button',{name:'Unlock Klefki',exact:true}).click();
  await expect(receiver.getByRole('button',{name:/Incoming documents/})).not.toContainText('1');
  await expect(receiver.locator('.incoming-toast')).toHaveCount(0);
 }finally{await second.close();c.wipe(fixture.master);}
});

test('unchanged checks avoid file inventory and identical metadata saves create no revision',async({page})=>{
 await c.ready;const fixture=c.createAccount(password);
 try{
  await open(page,fixture.account);await register(page);
  await upload(page,{name:'unchanged.txt',mimeType:'text/plain',buffer:Buffer.from('unchanged worker fixture')});
  await expect(page.getByRole('button',{name:'Open unchanged.txt',exact:true})).toBeVisible();
  await expect(page.getByRole('button',{name:'Sync status',exact:true})).toHaveAttribute('data-state','synced');
  const calls:string[]=[];page.on('request',r=>{const path=new URL(r.url()).pathname;if(path.startsWith('/v1/'))calls.push(path);});
  await page.waitForTimeout(6500);
  expect(calls).toContain('/v1/sync/check');expect(calls.some(path=>path.includes('/blobs/')||path.endsWith('/need-batch')||path.endsWith('/content/state'))).toBe(false);
  const count=()=>page.evaluate(async account=>{const db=await new Promise<IDBDatabase>(resolve=>{const q=indexedDB.open('klefki-v1-'+account);q.onsuccess=()=>resolve(q.result)});const count=await new Promise<number>(resolve=>{const q=db.transaction('revisions').objectStore('revisions').count();q.onsuccess=()=>resolve(q.result)});db.close();return count},fixture.account.account);
  const before=await count();const row=page.locator('article').filter({has:page.getByText('unchanged.txt',{exact:true})});await row.getByRole('button',{name:'Actions for unchanged.txt',exact:true}).click();await page.getByRole('menuitem',{name:'Edit details & move',exact:true}).click();await page.getByRole('button',{name:'Save changes',exact:true}).click();
  expect(await count()).toBe(before);
 }finally{c.wipe(fixture.master);}
});
