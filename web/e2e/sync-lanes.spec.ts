import {test,expect,type Page} from '@playwright/test';
import * as c from '../src/crypto.ts';
import {openSettings,backToLibrary,upload} from './ui.ts';
const password='public independent transfer lane fixture';
async function open(page:Page,account:c.Account){
 await page.goto('/');await page.getByLabel('Open an encrypted account file').setInputFiles({name:'account.klefki',mimeType:'application/octet-stream',buffer:Buffer.from(c.accountBytes(account))});
 await page.getByLabel('Password',{exact:true}).fill(password);await page.getByRole('button',{name:'Unlock Klefki',exact:true}).click();await expect(page.getByRole('heading',{name:'Documents',exact:true})).toBeVisible();
}
test('a new upload reclaims its lane while two older downloads are stalled',async({page,browser})=>{
 await c.ready;const fixture=c.createAccount(password),context=await browser.newContext(),receiver=await context.newPage();let release!:()=>void,downloads=0;
 const held=new Promise<void>(resolve=>{release=resolve});
 try{
  await open(page,fixture.account);await openSettings(page,'Sync & devices');await page.getByRole('button',{name:'Register this store on server',exact:true}).click();await expect(page.getByRole('status')).toContainText('Synced.');await backToLibrary(page);
  await open(receiver,fixture.account);await expect(receiver.getByRole('button',{name:'Sync status',exact:true})).toHaveAttribute('data-state','synced');
  await receiver.route('**/v1/blobs/*/chunks/*',async route=>{if(route.request().method()==='GET'){downloads++;await held;}await route.continue();});
  await page.getByLabel('Add document').setInputFiles(['a','b'].map(name=>({name:'older-'+name+'.txt',mimeType:'text/plain',buffer:Buffer.from('stalled download '+name)})));
  await expect.poll(()=>downloads,{timeout:12000}).toBeGreaterThanOrEqual(2);
  await upload(receiver,{name:'new-opposite-direction.txt',mimeType:'text/plain',buffer:Buffer.from('independent upload bytes')});
  const arrived=page.getByRole('button',{name:'Open new-opposite-direction.txt',exact:true});await expect(arrived).toBeVisible({timeout:12000});
  await expect(page.locator('article').filter({has:page.getByText('new-opposite-direction.txt',{exact:true})})).not.toContainText('Not stored on this device',{timeout:5000});
  await arrived.click();await expect(page.locator('dialog pre')).toHaveText('independent upload bytes');
 }finally{release();await context.close();c.wipe(fixture.master);}
});
