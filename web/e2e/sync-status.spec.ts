import {test,expect,type Page} from '@playwright/test';
import * as c from '../src/crypto.ts';
import {openSettings,upload,backToLibrary} from './ui.ts';

const password='public sync status fixture password';
async function open(page:Page,account:c.Account){
  await page.goto('/');
  await page.getByLabel('Open an encrypted account file').setInputFiles({name:'account.klefki',mimeType:'application/octet-stream',buffer:Buffer.from(c.accountBytes(account))});
  await page.getByLabel('Password',{exact:true}).fill(password);await page.getByRole('button',{name:'Unlock Klefki',exact:true}).click();
  await expect(page.getByRole('heading',{name:'Documents',exact:true})).toBeVisible();
}
async function localDevice(page:Page){
  return page.evaluate(async()=>{
    const account=JSON.parse(localStorage.getItem('klefki-account')!);
    const db=await new Promise<IDBDatabase>((resolve,reject)=>{const r=indexedDB.open('klefki-v1-'+account.account);r.onsuccess=()=>resolve(r.result);r.onerror=reject});
    const id=await new Promise<string>(resolve=>{const r=db.transaction('state').objectStore('state').get('relay-device');r.onsuccess=()=>resolve(r.result)});db.close();return id;
  });
}

test('automatic transfers show source, confirmed progress, private nicknames and device receipts',async({page,browser})=>{
  await c.ready;const created=c.createAccount(password),account=created.account,keys=c.unlock(account,password);
  const second=await browser.newContext(),b=await second.newPage();
  let release=()=>{};
  try{
    await open(page,account);await openSettings(page,'Sync & devices');
    await page.getByRole('button',{name:'Register this store on server',exact:true}).click();await expect(page.getByRole('status')).toContainText('Synced.');
    await open(b,account);await expect(b.getByRole('status')).toContainText('Synced.');
    const source=await localDevice(page);
    // Hold a real chunk request long enough to see progress before acknowledgement.
    const blocked=new Promise<void>(resolve=>{release=resolve});let held=false;
    await page.route('**/v1/blobs/*/chunks/0',async route=>{if(route.request().method()==='PUT'&&!held){held=true;await blocked;}await route.continue()});
    await upload(page,{name:'visible-progress.bin',mimeType:'application/octet-stream',buffer:Buffer.alloc(4194304+29,17)});
    await expect(page.getByRole('region',{name:'Sync progress'})).toContainText('Uploading');
    await expect(page.getByRole('progressbar',{name:'Document sync progress'})).toHaveAttribute('value','0');
    await expect(page.getByRole('region',{name:'Sync progress'})).toContainText('chunk 1 of 2');
    // Keyboard focus on Details must survive the one-second status ticks.
    await page.getByRole('button',{name:'Details',exact:true}).focus();await page.waitForTimeout(1200);await expect(page.getByRole('button',{name:'Details',exact:true})).toBeFocused();
    release();await expect(page.getByRole('status')).toContainText('Synced.');await page.unroute('**/v1/blobs/*/chunks/0');
    await b.getByRole('button',{name:'Sync status',exact:true}).click();
    const panel=b.getByRole('dialog',{name:'Sync activity'});await expect(panel.getByRole('textbox',{name:'Nickname for device '+source.slice(0,8)})).toBeVisible();
    await panel.getByRole('textbox',{name:'Nickname for device '+source.slice(0,8)}).fill('My laptop');await panel.getByRole('textbox',{name:'Nickname for device '+source.slice(0,8)}).locator('..').getByRole('button',{name:'Save',exact:true}).click();
    await expect(panel.getByText('My laptop',{exact:true})).toBeVisible();
    const recipient=await localDevice(b);
    let logins=0;await b.route('**/v1/login',async route=>{logins++;await route.continue()});
    await panel.getByRole('button',{name:'Refresh sync',exact:true}).click();await expect(panel.getByRole('button',{name:'Refresh sync',exact:true})).toBeEnabled();await expect(b.getByRole('status')).toContainText('Synced.');
    expect(logins).toBe(1);
    expect(await localDevice(b)).toBe(recipient);
    await expect(b.locator('article').filter({has:b.getByText('visible-progress.bin',{exact:true})})).toBeVisible();
    await expect(panel).toBeVisible();
    await expect(panel).toContainText('from My laptop via server');await expect(panel).toContainText('Received latest changes');
    const nameOnServer=await b.evaluate(async({account,credential})=>{
      const res=await fetch('/v1/login',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({account,credential})});const {token}=await res.json();
      return await(await fetch('/v1/sync/status',{headers:{Authorization:'Bearer '+token}})).text();
    },{account:account.account,credential:c.base64(keys.credential)});
    expect(nameOnServer).not.toContain('My laptop');expect(nameOnServer).not.toContain('visible-progress.bin');
    await panel.getByRole('button',{name:'Close',exact:true}).click();await b.getByRole('button',{name:'Lock',exact:true}).click();await expect(b.getByText(/visible-progress.bin/)).toHaveCount(0);
    await b.getByLabel('Password',{exact:true}).fill(password);await b.getByRole('button',{name:'Unlock Klefki',exact:true}).click();await expect(b.getByRole('heading',{name:'Documents',exact:true})).toBeVisible();
    await b.getByRole('button',{name:'Sync status',exact:true}).click();await expect(panel.getByRole('textbox',{name:'Nickname for device '+source.slice(0,8)})).toHaveValue('My laptop');
  } finally {release();await second.close().catch(()=>{});c.wipe(created.master);c.wipe(keys.master);c.wipe(keys.credential);}
});

test('compact indicator opens a centered menu with stable paginated activity',async({page})=>{
  await c.ready;const created=c.createAccount(password);
  try{
    await open(page,created.account);await openSettings(page,'Sync & devices');
    await page.getByRole('button',{name:'Register this store on server',exact:true}).click();await expect(page.getByRole('status')).toContainText('Synced.');
    await backToLibrary(page);
    await page.getByLabel('Add document').setInputFiles(Array.from({length:7},(_,i)=>({name:`history-${i}.txt`,mimeType:'text/plain',buffer:Buffer.from(`history ${i}`)})));
    await expect(page.locator('article')).toHaveCount(7);
    await expect(page.getByRole('button',{name:'Sync status',exact:true})).toHaveAttribute('data-state','synced');
    const trigger=page.getByRole('button',{name:'Sync status',exact:true});
    const size=await trigger.boundingBox();expect(size!.width).toBeGreaterThanOrEqual(44);expect(size!.height).toBeGreaterThanOrEqual(44);
    await expect(trigger.locator('.sr-only')).toHaveCSS('width','1px');
    await trigger.click();const panel=page.getByRole('dialog',{name:'Sync activity'});
    await expect(panel.locator('.sync-history li')).toHaveCount(5);
    await expect(panel.getByText('Page 1 of 2',{exact:true})).toBeVisible();
    await panel.getByRole('button',{name:'Next',exact:true}).click();
    await expect(panel.locator('.sync-history li')).toHaveCount(2);
    await panel.getByRole('button',{name:'Previous',exact:true}).focus();await page.waitForTimeout(1200);
    await expect(panel.getByText('Page 2 of 2',{exact:true})).toBeVisible();await expect(panel.getByRole('button',{name:'Previous',exact:true})).toBeFocused();
    for(const width of [320,390,600]){
      await page.setViewportSize({width,height:740});
      const rect=(await panel.boundingBox())!;
      expect(Math.abs(rect.x+rect.width/2-width/2)).toBeLessThanOrEqual(1);
      expect(Math.abs(rect.y+rect.height/2-370)).toBeLessThanOrEqual(1);
      expect(rect.x).toBeGreaterThanOrEqual(16);expect(rect.y).toBeGreaterThanOrEqual(16);
      expect(rect.y+rect.height).toBeLessThanOrEqual(724);
      expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
      await page.screenshot({path:`test-results/sync-menu-${width}.png`});
    }
    await panel.getByRole('button',{name:'Close',exact:true}).click();await trigger.click();
    await expect(panel.getByText('Page 1 of 2',{exact:true})).toBeVisible();
    await page.context().setOffline(true);await expect(trigger).toHaveAttribute('data-state','neutral');await expect(panel).toContainText('Network unavailable');
    await page.route('**/v1/sync/status',route=>route.fulfill({status:503,body:'Unavailable'}));
    await page.context().setOffline(false);await expect(trigger).toHaveAttribute('data-state','error');await expect(panel).toContainText('Server unreachable');
  }finally{await page.context().setOffline(false);c.wipe(created.master);}
});

test('unsupported device status does not stop sync and offline work stays queued',async({page})=>{
  await c.ready;const created=c.createAccount(password);
  try{
    await page.route('**/v1/sync/status',route=>route.fulfill({status:404,body:'Not Found'}));await page.route('**/v1/sync/provenance?*',route=>route.fulfill({status:404,body:'Not Found'}));
    await open(page,created.account);await openSettings(page,'Sync & devices');await page.getByRole('button',{name:'Register this store on server',exact:true}).click();await expect(page.getByRole('status')).toContainText('Synced.');
    await backToLibrary(page);await page.getByRole('button',{name:'Sync status',exact:true}).click();const panel=page.getByRole('dialog',{name:'Sync activity'});await expect(panel).toContainText('Device status unavailable');await panel.getByRole('button',{name:'Close',exact:true}).click();
    await page.context().setOffline(true);await upload(page,{name:'offline-status.txt',mimeType:'text/plain',buffer:Buffer.from('offline pending')});await expect(page.locator('article')).toContainText('offline-status.txt');
    await expect(page.getByRole('button',{name:'Sync status',exact:true})).toHaveAttribute('data-state','neutral');
    await expect(page.getByRole('button',{name:'Sync status',exact:true})).toHaveAttribute('title','Network unavailable');
    await page.context().setOffline(false);await expect(page.getByRole('status')).toContainText('Synced.');
  }finally{c.wipe(created.master);}
});
