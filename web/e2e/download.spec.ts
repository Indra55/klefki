import {test,expect,type Page} from '@playwright/test';
import {execFileSync} from 'node:child_process';
import {writeFile} from 'node:fs/promises';
import {ZipArchive} from '../src/zip.ts';
import * as c from '../src/crypto.ts';
import {backToLibrary,openSettings,upload} from './ui.ts';

const password='public bulk download fixture password';
async function open(page:Page,account:c.Account){
 await page.goto('/');await page.getByLabel('Open an encrypted account file').setInputFiles({name:'account.klefki',mimeType:'application/octet-stream',buffer:Buffer.from(c.accountBytes(account))});
 await page.getByLabel('Password',{exact:true}).fill(password);await page.getByRole('button',{name:'Unlock Klefki',exact:true}).click();
 await expect(page.getByRole('heading',{name:'Documents',exact:true})).toBeVisible();
 await openSettings(page,'Sync & devices');await page.getByRole('button',{name:'Pause sync',exact:true}).click();await backToLibrary(page);
}
async function folder(page:Page,name:string){await page.getByRole('button',{name:'New folder',exact:true}).click();await page.getByLabel('Folder name').fill(name);await page.getByRole('button',{name:'Create folder',exact:true}).click();await expect(page.getByRole('button',{name:'Open '+name,exact:true})).toBeVisible();}
async function root(page:Page){await page.getByRole('navigation',{name:'Folder breadcrumb'}).getByRole('button',{name:'All',exact:true}).click();}

test('one ZIP downloads multiple files and nested folders with safe distinct names',async({page})=>{
 await c.ready;const fixture=c.createAccount(password);
 try{
  await open(page,fixture.account);await folder(page,'Records');await page.getByRole('button',{name:'Open Records',exact:true}).click();await folder(page,'Empty');
  await upload(page,{name:'Résumé.txt',mimeType:'text/plain',buffer:Buffer.from('private café fixture')});await expect(page.getByRole('button',{name:'Open Résumé.txt',exact:true})).toBeVisible();await root(page);
  for(const [name,body] of [['report.txt','first report'],['REPORT.txt','second report'],['empty.txt',''],['exclude.txt','not selected']]){
   await upload(page,{name,mimeType:'text/plain',buffer:Buffer.from(body)});await expect(page.getByRole('button',{name:'Open '+name,exact:true})).toBeVisible();
  }
  await page.getByRole('button',{name:'Select items',exact:true}).click();
  for(const name of ['Records','report.txt','REPORT.txt','empty.txt'])await page.getByRole('checkbox',{name:'Select '+name,exact:true}).check();
  for(const width of [320,390,1200]){await page.setViewportSize({width,height:844});expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);await expect(page.getByRole('button',{name:'Download ZIP',exact:true})).toBeInViewport();}
  const event=page.waitForEvent('download');await page.getByRole('button',{name:'Download ZIP',exact:true}).click();const file=await event;
  expect(file.suggestedFilename()).toBe('klefki-documents.zip');
  const archive=JSON.parse(execFileSync('python3',['-c','import json,sys,zipfile\nwith zipfile.ZipFile(sys.argv[1]) as z:\n assert z.testzip() is None\n print(json.dumps({n:z.read(n).decode() for n in z.namelist()}))', (await file.path())!],{encoding:'utf8'}));
  expect(archive).toEqual({'Records/':'','Records/Empty/':'','Records/Résumé.txt':'private café fixture','report.txt':'first report','REPORT (2).txt':'second report','empty.txt':''});
  await expect(page.locator('#status')).toContainText('ZIP download started.');await expect(page.getByRole('checkbox',{name:'Select Records',exact:true})).toBeChecked();
 }finally{c.wipe(fixture.master);}
});

for(const damage of ['corrupt','missing'])test(`${damage} nested content produces no partial download and retains selection`,async({page})=>{
 await c.ready;const fixture=c.createAccount(password);const downloads:string[]=[];page.on('download',d=>downloads.push(d.suggestedFilename()));
 try{
  await open(page,fixture.account);await folder(page,'Records');await page.getByRole('button',{name:'Open Records',exact:true}).click();
  await upload(page,{name:'inside.txt',mimeType:'text/plain',buffer:Buffer.from('authenticated fixture')});await expect(page.getByRole('button',{name:'Open inside.txt',exact:true})).toBeVisible();await root(page);
  await page.evaluate(async({account,damage})=>{
   const db=await new Promise<IDBDatabase>(resolve=>{const q=indexedDB.open('klefki-v1-'+account);q.onsuccess=()=>resolve(q.result)});
   await new Promise<void>((resolve,reject)=>{const tx=db.transaction('chunks','readwrite'),q=tx.objectStore('chunks').openCursor();q.onsuccess=()=>{const row=q.result;if(!row)return;if(damage==='missing')row.delete();else{const b=new Uint8Array(row.value);b[b.length-1]^=1;row.update(b);}row.continue();};tx.oncomplete=()=>resolve();tx.onabort=()=>reject(tx.error)});db.close();
  },{account:fixture.account.account,damage});
  await page.getByRole('button',{name:'Select items',exact:true}).click();await page.getByRole('checkbox',{name:'Select Records',exact:true}).check();
  await page.getByRole('button',{name:'Download ZIP',exact:true}).click();await expect(page.locator('#status')).toContainText(damage==='corrupt'?'Could not download the selection':'Sync them before downloading');
  expect(downloads).toEqual([]);await expect(page.getByRole('checkbox',{name:'Select Records',exact:true})).toBeChecked();
 }finally{c.wipe(fixture.master);}
});

test('ZIP64 keeps every entry beyond the standard ZIP count limit',async({},testInfo)=>{
 const archive=new ZipArchive();for(let n=0;n<65536;n++)archive.add(`file-${n}.txt`,new Uint8Array());
 const path=testInfo.outputPath('many-files.zip');await writeFile(path,new Uint8Array(await archive.finish().arrayBuffer()));
 const count=execFileSync('python3',['-c','import sys,zipfile\nwith zipfile.ZipFile(sys.argv[1]) as z:\n assert z.testzip() is None\n assert z.read("file-65535.txt")==b""\n print(len(z.infolist()))',path],{encoding:'utf8'});
 expect(Number(count)).toBe(65536);
});

test('locking during ZIP preparation cancels the download',async({page})=>{
 await c.ready;const fixture=c.createAccount(password),downloads:string[]=[];page.on('download',d=>downloads.push(d.suggestedFilename()));
 try{
  await open(page,fixture.account);await folder(page,'Records');await upload(page,{name:'private.txt',mimeType:'text/plain',buffer:Buffer.from('canceled export fixture')});await expect(page.getByRole('button',{name:'Open private.txt',exact:true})).toBeVisible();
  await page.getByRole('button',{name:'Select items',exact:true}).click();await page.getByRole('button',{name:'Select all',exact:true}).click();
  await page.evaluate(()=>{
   const observer=new MutationObserver(()=>{if(document.querySelector('#status')?.textContent?.startsWith('Preparing ZIP')){observer.disconnect();[...document.querySelectorAll('button')].find(b=>b.textContent?.trim()==='Lock')!.click();}});
   observer.observe(document.querySelector('#status')!,{childList:true,subtree:true,characterData:true});
  });
  await page.getByRole('button',{name:'Download ZIP',exact:true}).click();await expect(page.getByRole('button',{name:'Unlock Klefki',exact:true})).toBeVisible();
  expect(downloads).toEqual([]);await expect(page.getByRole('checkbox')).toHaveCount(0);
 }finally{c.wipe(fixture.master);}
});
