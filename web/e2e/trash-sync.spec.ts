import {test,expect,type Page} from '@playwright/test';
import * as c from '../src/crypto.ts';
import {openSettings,backToLibrary,syncNow,upload,documentAction} from './ui.ts';

const password='public selective Trash sync fixture password';
async function open(page:Page,account:c.Account){
 await page.goto('/');
 await page.getByLabel('Open an encrypted account file').setInputFiles({name:'account.klefki',mimeType:'application/octet-stream',buffer:Buffer.from(c.accountBytes(account))});
 await page.getByLabel('Password',{exact:true}).fill(password);
 await page.getByRole('button',{name:'Unlock Klefki',exact:true}).click();
 await expect(page.getByRole('heading',{name:'Documents',exact:true})).toBeVisible();
}
async function syncComplete(page:Page){
 const receipt=page.waitForResponse(r=>r.url().includes('/v1/sync/cursor')&&r.request().method()==='PUT');
 await syncNow(page);await receipt;
 await expect(page.getByRole('button',{name:'Sync status',exact:true})).not.toHaveAttribute('data-state','running');
}
async function trash(page:Page){
 await backToLibrary(page);await page.getByRole('button',{name:'View',exact:true}).click();
 await page.getByRole('menuitem',{name:'Trash',exact:true}).click();
}
test('Trash content is off by default, device overrides win, and Restore fetches explicitly',async({page,browser})=>{
 test.setTimeout(45000);
 await c.ready;const fixture=c.createAccount(password),receiver=await browser.newContext(),other=await receiver.newPage();
 try{
  await open(page,fixture.account);await openSettings(page,'Sync & devices');
  await expect(page.getByLabel('Sync Trash for this vault',{exact:true})).toHaveValue('false');
  await expect(page.getByLabel('Sync Trash on this device',{exact:true})).toHaveValue('follow-vault');
  await page.getByRole('button',{name:'Register this store on server',exact:true}).click();
  await expect(page.getByRole('status')).toContainText('Synced.');
  await page.getByRole('button',{name:'Pause sync',exact:true}).click();
  await upload(page,{name:'recover.txt',mimeType:'text/plain',buffer:Buffer.from('recover these synthetic bytes')});
  await documentAction(page,'recover.txt','Move to Trash');
  await upload(page,{name:'active.txt',mimeType:'text/plain',buffer:Buffer.from('active synthetic bytes')});
  const traffic:string[]=[];page.on('request',r=>{if(r.url().includes('/v1/blobs/')&&r.method()==='PUT')traffic.push(r.url());});
  await openSettings(page,'Sync & devices');await page.locator('.settings-panel').getByRole('button',{name:'Resume sync',exact:true}).click();
  await expect(page.getByRole('status')).toContainText('Synced.');
  expect(traffic).toHaveLength(1);
  await open(other,fixture.account);await syncComplete(other);
  await expect(other.getByText('active.txt',{exact:true})).toBeVisible();
  await other.getByRole('button',{name:'Open active.txt',exact:true}).click();await expect(other.locator('dialog pre')).toHaveText('active synthetic bytes');
  await other.getByRole('button',{name:'Close',exact:true}).click();
  await trash(other);const row=other.locator('article').filter({has:other.getByText('recover.txt',{exact:true})});
  await expect(row).toContainText('Not stored on this device');
  await row.getByRole('button',{name:'Restore',exact:true}).click();
  await expect(other.getByRole('status')).toContainText('Reconnect a device');
  await expect(row).toContainText('Not stored on this device');
  await openSettings(other,'Sync & devices');await other.getByLabel('Sync Trash on this device',{exact:true}).selectOption('off');
  await openSettings(page,'Sync & devices');await page.getByLabel('Sync Trash for this vault',{exact:true}).selectOption('true');
  await expect(page.getByText('Trash file sync is on on this device.',{exact:false})).toBeVisible();
  await syncComplete(page);await expect(page.getByRole('status')).toContainText('Synced.');
  // The earlier explicit recovery request resumes even though background Trash
  // transfers remain off on the receiver.
  await syncComplete(other);await trash(other);await expect(row).not.toContainText('Not stored on this device');
  await row.getByRole('button',{name:'Restore',exact:true}).click();
  await expect(other.getByRole('status')).toContainText('Document restored.');
  await backToLibrary(other);await other.getByRole('button',{name:'View',exact:true}).click();await other.getByRole('menuitem',{name:'All documents',exact:true}).click();
  await other.getByRole('button',{name:'Open recover.txt',exact:true}).click();await expect(other.locator('dialog pre')).toHaveText('recover these synthetic bytes');
  await other.getByRole('button',{name:'Close',exact:true}).click();
  await openSettings(other,'Sync & devices');await expect(other.getByLabel('Sync Trash on this device',{exact:true})).toHaveValue('off');
  await other.reload();await other.getByLabel('Password',{exact:true}).fill(password);await other.getByRole('button',{name:'Unlock Klefki',exact:true}).click();
  await openSettings(other,'Sync & devices');await expect(other.getByLabel('Sync Trash on this device',{exact:true})).toHaveValue('off');
 }finally{await receiver.close().catch(()=>{});c.wipe(fixture.master);}
});
