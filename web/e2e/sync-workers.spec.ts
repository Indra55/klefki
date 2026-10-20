import {test,expect,type Page} from '@playwright/test';
import * as c from '../src/crypto.ts';
import {openSettings,backToLibrary,upload,documentAction} from './ui.ts';
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

test('an in-flight library update does not block a new local upload',async({page,browser})=>{
 await c.ready;const fixture=c.createAccount(password),context=await browser.newContext(),receiver=await context.newPage();let release!:()=>void;
 const held=new Promise<void>(resolve=>{release=resolve});let libraryRequests=0;
 try{
  await open(page,fixture.account);await register(page);await open(receiver,fixture.account);
  await expect(receiver.getByRole('button',{name:'Sync status',exact:true})).toHaveAttribute('data-state','synced');
  await page.route('**/v1/sync/changes?*',async route=>{libraryRequests++;await held;await route.continue();});
  await upload(receiver,{name:'remote-trigger.txt',mimeType:'text/plain',buffer:Buffer.from('remote discovery trigger')});
  await expect.poll(()=>libraryRequests).toBeGreaterThan(0);
  await upload(page,{name:'new-during-library-update.txt',mimeType:'text/plain',buffer:Buffer.from('independent upload fixture')});
  await expect(receiver.getByRole('button',{name:'Open new-during-library-update.txt',exact:true})).toBeVisible({timeout:12000});
  await expect(receiver.locator('article').filter({has:receiver.getByText('new-during-library-update.txt',{exact:true})})).not.toContainText('Not stored on this device',{timeout:5000});
 }finally{release();await context.close();c.wipe(fixture.master);}
});

test('a failed older metadata publication yields to new documents during its retry delay',async({page,browser})=>{
 await c.ready;const fixture=c.createAccount(password),context=await browser.newContext(),receiver=await context.newPage();let failures=0,dependents=0;
 try{
  await open(page,fixture.account);await register(page);await open(receiver,fixture.account);
  await expect(receiver.getByRole('button',{name:'Sync status',exact:true})).toHaveAttribute('data-state','synced');
  await page.route('**/v1/sync/revisions/*',async route=>{
   const revision=route.request().postDataJSON();
   const name=c.decryptMetadata(fixture.master,fixture.account.account,revision.object,c.unbase64(revision.metadata)).name;
   if(name==='old-metadata.txt'){failures++;await route.fulfill({status:503,headers:{'Retry-After':'30'},body:''});return;}
   if(name==='dependent-metadata.txt')dependents++;
   await route.continue();
  });
  await upload(page,{name:'old-metadata.txt',mimeType:'text/plain',buffer:Buffer.from('old publication fixture')});await expect.poll(()=>failures).toBe(1);
  await documentAction(page,'old-metadata.txt','Edit details & move');await page.getByLabel('Name',{exact:true}).fill('dependent-metadata.txt');await page.getByRole('button',{name:'Save changes',exact:true}).click();
  await upload(page,{name:'new-past-metadata-retry.txt',mimeType:'text/plain',buffer:Buffer.from('new publication fixture')});
  await expect(receiver.getByRole('button',{name:'Open new-past-metadata-retry.txt',exact:true})).toBeVisible({timeout:12000});
  await expect(receiver.locator('article').filter({has:receiver.getByText('new-past-metadata-retry.txt',{exact:true})})).not.toContainText('Not stored on this device',{timeout:5000});
  expect(failures).toBe(1);expect(dependents).toBe(0);
  await page.reload();await page.getByLabel('Password',{exact:true}).fill(password);await page.getByRole('button',{name:'Unlock Klefki',exact:true}).click();
  await upload(page,{name:'after-reopen.txt',mimeType:'text/plain',buffer:Buffer.from('durable metadata retry fixture')});await expect(receiver.getByRole('button',{name:'Open after-reopen.txt',exact:true})).toBeVisible({timeout:12000});
  expect(failures).toBe(1);expect(dependents).toBe(0);
 }finally{await context.close();c.wipe(fixture.master);}
});

for(const scope of ['status','library','data'] as const)test(`${scope} rate limits respect their request budget`,async({page,browser})=>{
 await c.ready;const fixture=c.createAccount(password),context=await browser.newContext(),receiver=await context.newPage();let failures=0,published=0;
 try{
  await open(page,fixture.account);await register(page);await open(receiver,fixture.account);await expect(receiver.getByRole('button',{name:'Sync status',exact:true})).toHaveAttribute('data-state','synced');
  const limited=scope==='status'?'**/v1/sync/status?*':'**/v1/sync/changes?*';
  await page.route(limited,async route=>{failures++;await route.fulfill({status:429,headers:{'Retry-After':'30',...(scope==='library'?{'X-Klefki-Retry-Scope':'library'}:{})},body:''});});
  if(scope==='status'){
   await page.getByRole('button',{name:'Sync status',exact:true}).click();await expect.poll(()=>failures).toBe(1);await page.getByRole('dialog',{name:'Sync activity'}).getByRole('button',{name:'Close',exact:true}).click();
  }else{await upload(receiver,{name:'rate-trigger.txt',mimeType:'text/plain',buffer:Buffer.from('rate trigger fixture')});await expect.poll(()=>failures).toBe(1);}
  page.on('request',request=>{if(request.method()==='PUT'&&request.url().includes('/sync/revisions/'))published++;});
  await upload(page,{name:'new-during-rate-limit.txt',mimeType:'text/plain',buffer:Buffer.from('rate budget fixture')});await expect(page.getByRole('button',{name:'Open new-during-rate-limit.txt',exact:true})).toBeVisible();
  if(scope==='data'){
   await page.waitForTimeout(2000);expect(published).toBe(0);await page.getByRole('button',{name:'Sync status',exact:true}).click();await expect(page.getByRole('dialog',{name:'Sync activity'})).toContainText('Server rate limit');
  }else{
   await expect(receiver.getByRole('button',{name:'Open new-during-rate-limit.txt',exact:true})).toBeVisible({timeout:12000});await expect(receiver.locator('article').filter({has:receiver.getByText('new-during-rate-limit.txt',{exact:true})})).not.toContainText('Not stored on this device',{timeout:5000});
  }
  expect(failures).toBe(1);
 }finally{await context.close();c.wipe(fixture.master);}
});

test('an older library read cannot hide a document that already arrived',async({page,browser})=>{
 await c.ready;const fixture=c.createAccount(password),context=await browser.newContext(),receiver=await context.newPage();
 try{
  await open(page,fixture.account);await register(page);await open(receiver,fixture.account);
  await expect(receiver.getByRole('button',{name:'Sync status',exact:true})).toHaveAttribute('data-state','synced');
  await receiver.evaluate(()=>{
   const original=IDBObjectStore.prototype.getAll,success=Object.getOwnPropertyDescriptor(IDBRequest.prototype,'onsuccess')!;let held=false;
   IDBObjectStore.prototype.getAll=function(...args){
    const request=original.apply(this,args);if(this.name!=='revisions')return request;
    Object.defineProperty(request,'onsuccess',{set(handler){success.set!.call(request,function(event:Event){
     if(!held&&request.result.length===1&&request.result[0].deferred){held=true;(window as any).oldLibraryReadHeld=true;(window as any).releaseOldLibraryRead=()=>handler.call(request,event);}
     else handler.call(request,event);
    });}});return request;
   };
  });
  await upload(page,{name:'first-arrival.txt',mimeType:'text/plain',buffer:Buffer.from('first fixture')});
  await expect.poll(()=>receiver.evaluate(()=>(window as any).oldLibraryReadHeld)).toBe(true);
  await expect(receiver.getByRole('button',{name:'Open first-arrival.txt',exact:true})).toBeVisible();
  await upload(page,{name:'scan_new.txt',mimeType:'text/plain',buffer:Buffer.from('latest fixture')});
  const newest=receiver.getByRole('button',{name:'Open scan_new.txt',exact:true});await expect(newest).toBeVisible({timeout:20000});
  await expect(receiver.locator('.incoming-toast')).toContainText('scan_new.txt is ready');
  await expect(receiver.getByRole('button',{name:'Sync status',exact:true})).not.toHaveAttribute('data-state','running');
  await receiver.evaluate(()=>(window as any).releaseOldLibraryRead());await receiver.waitForTimeout(750);
  await expect(newest).toBeVisible();await newest.click();await expect(receiver.locator('dialog pre')).toHaveText('latest fixture');
 }finally{await context.close();c.wipe(fixture.master);}
});

test('switching away flushes a queued upload without waiting for a background timer',async({page,browser})=>{
 await c.ready;const fixture=c.createAccount(password),context=await browser.newContext(),receiver=await context.newPage();
 try{
  await page.addInitScript(()=>{
   const timeout=window.setTimeout.bind(window),interval=window.setInterval.bind(window);
   window.setTimeout=(handler,delay,...args)=>timeout(handler,delay===1000?600000:delay,...args);
   window.setInterval=(handler,delay,...args)=>interval(handler,delay===5000?600000:delay,...args);
  });
  await open(page,fixture.account);await register(page);await open(receiver,fixture.account);
  await expect(receiver.getByRole('button',{name:'Sync status',exact:true})).toHaveAttribute('data-state','synced');
  await expect(page.getByRole('button',{name:'Sync status',exact:true})).toHaveAttribute('data-state','synced');
  let uploads=0;page.on('request',request=>{if(request.method()==='PUT'&&request.url().includes('/chunks/'))uploads++;});
  await upload(page,{name:'phone-handoff.txt',mimeType:'text/plain',buffer:Buffer.from('handoff fixture')});
  await expect(page.getByRole('button',{name:'Open phone-handoff.txt',exact:true})).toBeVisible();
  expect(uploads).toBe(0);
  await page.evaluate(()=>{Object.defineProperty(document,'visibilityState',{configurable:true,get:()=> 'hidden'});document.dispatchEvent(new Event('visibilitychange'));});
  await expect.poll(()=>uploads,{timeout:5000}).toBeGreaterThan(0);
  await expect(receiver.getByRole('button',{name:'Open phone-handoff.txt',exact:true})).toBeVisible({timeout:15000});
 }finally{await context.close();c.wipe(fixture.master);}
});

test('relay receivers show new documents and metadata moves without refresh',async({page,browser})=>{
 await c.ready;const fixture=c.createAccount(password),context=await browser.newContext(),receiver=await context.newPage();
 try{
  await open(page,fixture.account);await register(page);await openSettings(page,'Sync & devices');
  await page.getByLabel('Sync mode',{exact:true}).selectOption('temporary-relay');await page.getByRole('button',{name:'Apply sync mode',exact:true}).click();await expect(page.locator('#status')).toContainText('Sync mode updated');await backToLibrary(page);
  await open(receiver,fixture.account);await expect(receiver.getByRole('button',{name:'Sync status',exact:true})).toHaveAttribute('data-state','synced');
  await page.getByRole('button',{name:'New folder',exact:true}).click();await page.getByLabel('Folder name').fill('Destination');await page.getByRole('button',{name:'Create folder',exact:true}).click();
  await expect(receiver.getByRole('button',{name:'Open Destination',exact:true})).toBeVisible({timeout:12000});
  await upload(page,{name:'scan_new.txt',mimeType:'text/plain',buffer:Buffer.from('automatic metadata fixture')});await expect(receiver.getByRole('button',{name:'Open scan_new.txt',exact:true})).toBeVisible({timeout:12000});
  await documentAction(page,'scan_new.txt','Edit details & move');await page.getByLabel('Name',{exact:true}).fill('renamed.txt');await page.getByLabel('Folder',{exact:true}).selectOption({label:'Destination'});await page.getByRole('button',{name:'Save changes',exact:true}).click();
  await expect(receiver.getByRole('button',{name:'Open scan_new.txt',exact:true})).toHaveCount(0,{timeout:12000});await receiver.getByRole('button',{name:'Open Destination',exact:true}).click();
  await expect(receiver.locator('article').filter({has:receiver.getByText('renamed.txt',{exact:true})})).not.toContainText('Not stored on this device');
  await expect(receiver.getByRole('button',{name:'Open renamed.txt',exact:true})).toBeVisible();await receiver.getByRole('button',{name:'Open renamed.txt',exact:true}).click();await expect(receiver.locator('dialog pre')).toHaveText('automatic metadata fixture');
 }finally{await context.close();c.wipe(fixture.master);}
});
