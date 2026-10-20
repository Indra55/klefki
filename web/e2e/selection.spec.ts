import {test,expect,type Page} from '@playwright/test';
import * as c from '../src/crypto.ts';
import {backToLibrary,documentAction,openSettings,syncNow,upload} from './ui.ts';

const password='public bulk transfer fixture password';
async function open(page:Page,account:c.Account){
 await page.goto('/');
 await page.getByLabel('Open an encrypted account file').setInputFiles({name:'account.klefki',mimeType:'application/octet-stream',buffer:Buffer.from(c.accountBytes(account))});
 await page.getByLabel('Password',{exact:true}).fill(password);
 await page.getByRole('button',{name:'Unlock Klefki',exact:true}).click();
 await expect(page.locator('.library')).toBeVisible();
}
async function folder(page:Page,name:string){
 await page.getByRole('button',{name:'New folder',exact:true}).click();
 await page.getByLabel('Folder name').fill(name);
 await page.getByRole('button',{name:'Create folder',exact:true}).click();
 await expect(page.getByRole('button',{name:'Open '+name,exact:true})).toBeVisible();
}
async function root(page:Page){await page.getByRole('navigation',{name:'Folder breadcrumb'}).getByRole('button',{name:'All',exact:true}).click();}
async function select(page:Page,names:string[]){
 await page.getByRole('button',{name:'Select items',exact:true}).click();
 for(const name of names)await page.getByRole('checkbox',{name:'Select '+name,exact:true}).check();
 await expect(page.getByRole('toolbar',{name:'Selected items'})).toContainText(names.length+' selected');
}
async function transfer(page:Page,copy:boolean,destination:string){
 await page.getByRole('button',{name:copy?'Copy to…':'Move to…',exact:true}).click();
 const dialog=page.getByRole('dialog',{name:copy?'Copy items':'Move items',exact:true});
 await dialog.getByLabel('Destination folder').selectOption({label:destination});
 await dialog.getByRole('button',{name:copy?'Copy here':'Move here',exact:true}).click();
 await expect(dialog).toHaveCount(0);
}
async function text(page:Page,name:string,expected:string){
 await expect(page.locator('article').filter({has:page.getByText(name,{exact:true})})).not.toContainText('Not stored on this device');
 await page.getByRole('button',{name:'Open '+name,exact:true}).click();
 await expect(page.locator('dialog pre')).toHaveText(expected);
 await page.getByRole('button',{name:'Close',exact:true}).click();
}

test('mixed recursive copy and move preserve independent content through encrypted sync',async({page,browser})=>{
 await c.ready;const fixture=c.createAccount(password),receiver=await browser.newContext(),other=await receiver.newPage();
 try{
  await open(page,fixture.account);await openSettings(page,'Sync & devices');
  await page.getByRole('button',{name:'Register this store on server',exact:true}).click();
  await expect(page.getByRole('status')).toContainText('Synced.');
  await page.getByRole('button',{name:'Pause sync',exact:true}).click();await backToLibrary(page);
  await folder(page,'Archive');await folder(page,'Copies');await folder(page,'Records');
  await page.getByRole('button',{name:'Open Records',exact:true}).click();await folder(page,'Nested');
  await page.getByRole('button',{name:'Open Nested',exact:true}).click();
  await upload(page,{name:'inside.txt',mimeType:'text/plain',buffer:Buffer.from('independent nested content')});
  await root(page);await upload(page,{name:'loose.txt',mimeType:'text/plain',buffer:Buffer.from('independent loose content')});
  await select(page,['Records','loose.txt']);
  for(const width of [320,390,1200]){
   await page.setViewportSize({width,height:844});
   expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
   await expect(page.getByRole('button',{name:'Copy to…',exact:true})).toBeInViewport();
  }
  await page.screenshot({path:'/tmp/klefki-selection-web.png'});
  await page.getByRole('button',{name:'Copy to…',exact:true}).click();
  const dialog=page.getByRole('dialog',{name:'Copy items'});
  await expect(dialog.getByRole('option')).toHaveText(['All / Archive','All / Copies']);
  await dialog.getByLabel('Destination folder').selectOption({label:'All / Copies'});
  await dialog.getByRole('button',{name:'Cancel',exact:true}).click();
  await expect(page.getByRole('checkbox',{name:'Select Records',exact:true})).toBeChecked();
  await transfer(page,true,'All / Copies');
  await select(page,['Records','loose.txt']);await transfer(page,false,'All / Archive');
  await expect(page.getByRole('button',{name:'Open Records',exact:true})).toHaveCount(0);
  await page.getByRole('button',{name:'Open Copies',exact:true}).click();await text(page,'loose.txt','independent loose content');
  await page.getByRole('button',{name:'Open Records',exact:true}).click();await page.getByRole('button',{name:'Open Nested',exact:true}).click();
  await text(page,'inside.txt','independent nested content');
  await select(page,['inside.txt']);await transfer(page,false,'Library root');
  await root(page);await text(page,'inside.txt','independent nested content');
  await documentAction(page,'inside.txt','Edit details & move');
  await page.getByRole('button',{name:'Edit text content',exact:true}).click();
  await page.getByLabel('Document text').fill('edited independent copy');await page.getByRole('button',{name:'Save changes',exact:true}).click();
  await openSettings(page,'Sync & devices');await page.getByRole('main').getByRole('button',{name:'Resume sync',exact:true}).click();
  await expect(page.getByRole('status')).toContainText('Synced.');
  await open(other,fixture.account);await syncNow(other);
  await expect(other.getByRole('button',{name:'Open Archive',exact:true})).toBeVisible();
  await text(other,'inside.txt','edited independent copy');
  await other.getByRole('button',{name:'Open Archive',exact:true}).click();await text(other,'loose.txt','independent loose content');
  await other.getByRole('button',{name:'Open Records',exact:true}).click();await other.getByRole('button',{name:'Open Nested',exact:true}).click();
  await text(other,'inside.txt','independent nested content');
 }finally{await receiver.close();c.wipe(fixture.master);}
});

test('a corrupt nested file leaves no partial copy and keeps the selection',async({page})=>{
 await c.ready;const fixture=c.createAccount(password);
 try{
  await open(page,fixture.account);await openSettings(page,'Sync & devices');
  await page.getByRole('button',{name:'Pause sync',exact:true}).click();await backToLibrary(page);
  await folder(page,'Destination');await folder(page,'Empty');await folder(page,'Records');
  await page.getByRole('button',{name:'Open Records',exact:true}).click();
  await upload(page,{name:'damaged.txt',mimeType:'text/plain',buffer:Buffer.from('synthetic corrupt copy fixture')});await root(page);
  const before=await page.evaluate(async account=>{
   const db=await new Promise<IDBDatabase>((resolve,reject)=>{const r=indexedDB.open('klefki-v1-'+account);r.onsuccess=()=>resolve(r.result);r.onerror=()=>reject(r.error);});
   const result=await new Promise<{revisions:number;chunks:number}>((resolve,reject)=>{
    const tx=db.transaction(['chunks','revisions'],'readwrite'),chunks=tx.objectStore('chunks');let count=0,revisions=0;
    const r=chunks.openCursor();r.onsuccess=()=>{const cursor=r.result;if(cursor){const bytes=new Uint8Array(cursor.value);bytes[bytes.length-1]^=1;cursor.update(bytes);count++;cursor.continue();}};
    const n=tx.objectStore('revisions').count();n.onsuccess=()=>{revisions=n.result;};tx.oncomplete=()=>resolve({revisions,chunks:count});tx.onerror=()=>reject(tx.error);
   });db.close();return result;
  },fixture.account.account);
  await select(page,['Empty','Records']);await page.getByRole('button',{name:'Copy to…',exact:true}).click();
  const dialog=page.getByRole('dialog',{name:'Copy items'});await dialog.getByLabel('Destination folder').selectOption({label:'All / Destination'});
  await dialog.getByRole('button',{name:'Copy here',exact:true}).click();await expect(dialog.getByRole('alert')).not.toBeEmpty();
  const after=await page.evaluate(async account=>{
   const db=await new Promise<IDBDatabase>(resolve=>{const r=indexedDB.open('klefki-v1-'+account);r.onsuccess=()=>resolve(r.result);});
   const counts=await Promise.all(['revisions','chunks'].map(name=>new Promise<number>(resolve=>{const r=db.transaction(name).objectStore(name).count();r.onsuccess=()=>resolve(r.result);})));db.close();return {revisions:counts[0],chunks:counts[1]};
  },fixture.account.account);
  expect(after).toEqual(before);
  await dialog.getByRole('button',{name:'Cancel',exact:true}).click();await expect(page.getByRole('checkbox',{name:'Select Records',exact:true})).toBeChecked();
  await page.getByRole('button',{name:'Cancel selection',exact:true}).click();await page.getByRole('button',{name:'Open Destination',exact:true}).click();
  await expect(page.locator('article')).toHaveCount(0);
 }finally{c.wipe(fixture.master);}
});
