import {test,expect,type Page} from '@playwright/test';
import * as c from '../src/crypto.ts';
import {openSettings,backToLibrary,upload,syncNow,documentAction} from './ui.ts';

const password='public sync pause fixture password';
async function unlock(page:Page){
 await page.getByLabel('Password',{exact:true}).fill(password);
 await page.getByRole('button',{name:'Unlock Klefki',exact:true}).click();
 await expect(page.locator('.library')).toBeVisible();
}
async function open(page:Page,account:c.Account){
 await page.goto('/');
 await page.getByLabel('Open an encrypted account file').setInputFiles({name:'account.klefki',mimeType:'application/octet-stream',buffer:Buffer.from(c.accountBytes(account))});
 await unlock(page);
}
const status=(page:Page)=>page.getByRole('button',{name:'Sync status',exact:true});

for(const mode of ['persistent','temporary-relay'])test(`pause survives reload and reconnect, queues a batch and resumes encrypted sync in both directions (${mode})`,async({page,browser})=>{
 await c.ready;const created=c.createAccount(password),other=await browser.newContext(),b=await other.newPage();
 try{
  await page.setViewportSize({width:390,height:844});
  await open(page,created.account);await openSettings(page,'Sync & devices');
  await page.getByRole('button',{name:'Register this store on server',exact:true}).click();
  await expect(page.getByRole('status')).toContainText('Synced.');
  if(mode==='temporary-relay'){await page.getByLabel('Sync mode',{exact:true}).selectOption(mode);await page.getByRole('button',{name:'Apply sync mode',exact:true}).click();await expect(page.getByRole('status')).toContainText('Sync mode updated');}
  await open(b,created.account);await expect(status(b)).toHaveAttribute('data-state',/^(synced|waiting)$/);
  await page.getByRole('button',{name:'Pause sync',exact:true}).click();
  await expect(status(page)).toHaveAttribute('aria-description','Sync paused on this device');
  await expect(page.getByRole('button',{name:'Sync now',exact:true})).toBeDisabled();
  let transfers=0;
  page.on('request',r=>{if(new URL(r.url()).pathname.startsWith('/v1/'))transfers++});
  for(const width of [320,390,600]){await page.setViewportSize({width,height:844});expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);}
  await page.screenshot({path:'/tmp/klefki-web-sync-pause.png',fullPage:true});
  await backToLibrary(page);await expect(page.locator('.library')).toBeVisible();
  for(const name of ['Batch one.txt','Batch two.txt'])await upload(page,{name,mimeType:'text/plain',buffer:Buffer.from('public queued fixture '+name)});
  await expect(page.getByRole('button',{name:'Open Batch two.txt',exact:true})).toBeVisible();
  await documentAction(page,'Batch one.txt','Edit details & move');
  await page.getByLabel('Name',{exact:true}).fill('Renamed batch.txt');
  await page.getByRole('button',{name:'Save changes',exact:true}).click();
  await expect(page.getByRole('button',{name:'Open Renamed batch.txt',exact:true})).toBeVisible();
  await upload(b,{name:'Incoming.txt',mimeType:'text/plain',buffer:Buffer.from('public incoming fixture')});
  await expect(status(b)).toHaveAttribute('data-state',/^(synced|waiting)$/);
  await page.evaluate(()=>{window.dispatchEvent(new Event('online'));document.dispatchEvent(new Event('visibilitychange'))});
  // Advance the real app's poll clock while keeping Playwright assertions real-time.
  await page.clock.install();await page.clock.fastForward(65000);
  expect(transfers).toBe(0);
  await expect(page.getByRole('button',{name:'Open Incoming.txt',exact:true})).toHaveCount(0);
  await syncNow(b);await expect(b.getByRole('button',{name:'Open Renamed batch.txt',exact:true})).toHaveCount(0);
  await page.reload();await unlock(page);
  await expect(status(page)).toHaveAttribute('aria-description','Sync paused on this device');
  await expect(page.getByRole('button',{name:'Open Renamed batch.txt',exact:true})).toBeVisible();
  await page.getByRole('button',{name:'Sync status',exact:true}).click();
  const panel=page.getByRole('dialog',{name:'Sync activity'});
  await expect(panel.getByRole('button',{name:'Refresh sync',exact:true})).toBeDisabled();
  await expect(panel).toContainText('changes waiting to upload');
  expect(transfers).toBe(0);
  await panel.getByRole('button',{name:'Resume sync',exact:true}).click();
  await panel.getByRole('button',{name:'Close',exact:true}).click();
  await expect(page.getByRole('button',{name:'Open Incoming.txt',exact:true})).toBeVisible();
  await expect(status(page)).toHaveAttribute('data-state',/^(synced|waiting)$/);
  await syncNow(b);
  await expect(b.getByRole('button',{name:'Open Renamed batch.txt',exact:true})).toBeVisible();
  await expect(b.getByRole('button',{name:'Open Batch two.txt',exact:true})).toBeVisible();
  await b.getByRole('button',{name:'Open Renamed batch.txt',exact:true}).click();
  await expect(b.locator('dialog pre')).toHaveText('public queued fixture Batch one.txt');
  await b.getByRole('button',{name:'Close',exact:true}).click();
  await page.getByRole('button',{name:'Open Incoming.txt',exact:true}).click();
  await expect(page.locator('dialog pre')).toHaveText('public incoming fixture');
  expect(transfers).toBeGreaterThan(0);
 }finally{await other.close();c.wipe(created.master);}
});

test('pausing during a sync finishes that cycle and suppresses a queued refresh',async({page})=>{
 await c.ready;const created=c.createAccount(password);let release=()=>{};
 try{
  await open(page,created.account);await openSettings(page,'Sync & devices');
  await page.getByRole('button',{name:'Register this store on server',exact:true}).click();
  await expect(page.getByRole('status')).toContainText('Synced.');
  await backToLibrary(page);
  const blocked=new Promise<void>(resolve=>{release=resolve});let held=false,changes=0;
  await page.route('**/v1/sync/changes?*',async route=>{changes++;if(!held){held=true;await blocked;}await route.continue()});
  await page.evaluate(()=>window.dispatchEvent(new Event('online')));
  await expect.poll(()=>held).toBe(true);
  await page.evaluate(()=>window.dispatchEvent(new Event('online')));
  await status(page).click();const panel=page.getByRole('dialog',{name:'Sync activity'});
  await panel.getByRole('button',{name:'Pause sync',exact:true}).click();
  await expect(status(page)).toHaveAttribute('aria-description','Pausing after current sync');
  release();
  await expect(status(page)).toHaveAttribute('aria-description','Sync paused on this device');
  await expect(panel.getByRole('button',{name:'Refresh sync',exact:true})).toBeDisabled();
  const before=changes;
  await page.evaluate(()=>{window.dispatchEvent(new Event('online'));document.dispatchEvent(new Event('visibilitychange'))});
  await expect(status(page)).toHaveAttribute('aria-description','Sync paused on this device');
  expect(changes).toBe(before);
  // A close event can arrive after native focus restoration and a user's next action.
  await panel.evaluate(element=>{const dialog=element as HTMLDialogElement,onclose=dialog.onclose!;dialog.onclose=event=>{setTimeout(()=>onclose.call(dialog,event),100);};});
  await panel.getByRole('button',{name:'Close',exact:true}).click();
  await expect(status(page)).toBeFocused();
  await expect(page.getByRole('button',{name:'Resume sync',exact:true})).toBeVisible();
  await page.getByRole('button',{name:'Resume sync',exact:true}).focus();
  await page.waitForTimeout(1200);
  await expect(page.getByRole('button',{name:'Resume sync',exact:true})).toBeFocused();
 }finally{release();c.wipe(created.master);}
});
