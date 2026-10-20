import {test,expect} from '@playwright/test';
import * as c from '../src/crypto.ts';

test('upgrades an existing v1 store and gives legacy trash a fresh countdown',async({page,context})=>{
 await c.ready;
 const password='public legacy trash fixture password',fixture=c.createAccount(password),account=fixture.account,fileKey=c.random(32);
 try{
  const root=c.id(),object=c.id(),file=c.id(),body=c.utf8('legacy private bytes');
  const folder={id:c.id(),object:root,base:'',metadata:c.base64(c.encryptMetadata(fixture.master,account.account,root,{type:'folder',name:'Trash',parent:null,tags:['klefki:system:trash'],favorite:false}))};
  const revision={id:c.id(),object,base:'',metadata:c.base64(c.encryptMetadata(fixture.master,account.account,object,{type:'file',name:'legacy.txt',parent:root,tags:[],favorite:false,file_id:file,size:body.length,chunk_count:1})),file_key:c.base64(c.seal(fixture.master,{kind:3,account:account.account,object:file},fileKey))};
  const chunk=c.seal(fileKey,{kind:5,account:account.account,object:file,index:0,final:true},body);
  await page.goto('/');
  await page.evaluate(async({account,folder,revision,file,chunk})=>{
   const db=await new Promise<IDBDatabase>((resolve,reject)=>{const q=indexedDB.open('klefki-v1-'+account.account,1);q.onupgradeneeded=()=>{q.result.createObjectStore('state');q.result.createObjectStore('revisions',{keyPath:'revision.id'});q.result.createObjectStore('chunks');};q.onsuccess=()=>resolve(q.result);q.onerror=()=>reject(q.error);});
   await new Promise<void>((resolve,reject)=>{const tx=db.transaction(['state','revisions','chunks'],'readwrite');tx.objectStore('state').put(account,'account');tx.objectStore('state').put(2,'order');tx.objectStore('revisions').put({revision:folder,pending:false,order:1});tx.objectStore('revisions').put({revision,pending:false,order:2});tx.objectStore('chunks').put(new Uint8Array(chunk),file+':0');tx.oncomplete=()=>resolve();tx.onabort=()=>reject(tx.error);});
   db.close();localStorage.setItem('klefki-account',JSON.stringify(account));
  },{account,folder,revision,file,chunk:Array.from(chunk)});
  await page.reload();await page.getByLabel('Password',{exact:true}).fill(password);await page.getByRole('button',{name:'Unlock Klefki',exact:true}).click();
  await page.getByRole('button',{name:'View',exact:true}).click();await page.getByRole('menuitem',{name:'Trash',exact:true}).click();
  await expect(page.locator('article').filter({has:page.getByText('legacy.txt',{exact:true})})).toContainText('Deletes');
  await page.getByRole('button',{name:'Open legacy.txt',exact:true}).click();await expect(page.locator('dialog pre')).toHaveText('legacy private bytes');
  const saved=await page.evaluate(async account=>{const db=await new Promise<IDBDatabase>(resolve=>{const q=indexedDB.open('klefki-v1-'+account);q.onsuccess=()=>resolve(q.result);});const version=db.version;const read=(name:string)=>new Promise<any[]>(resolve=>{const q=db.transaction(name).objectStore(name).getAll();q.onsuccess=()=>resolve(q.result);});const revisions=await read('revisions'),chunks=await read('chunks');db.close();return {version,revisions,chunk:Array.from(chunks[0])};},account.account);
  expect(saved.version).toBe(3);expect(saved.revisions.length).toBe(3);expect(saved.chunk).toEqual(Array.from(chunk));
  expect(JSON.stringify(saved.revisions)).not.toContain('legacy.txt');expect(JSON.stringify(saved.revisions)).not.toContain('legacy private bytes');
  const current=saved.revisions.find(x=>x.revision.base===revision.id);const metadata=c.decryptMetadata(fixture.master,account.account,object,c.unbase64(current.revision.metadata));
  expect(metadata.trashed_at).toBeGreaterThan(Date.now()-120000);expect(metadata.trashed_at).toBeLessThanOrEqual(Date.now());
  await page.getByRole('button',{name:'Close',exact:true}).click();await context.setOffline(true);
  const row=page.locator('article').filter({has:page.getByText('legacy.txt',{exact:true})});await row.getByRole('button',{name:'Actions for legacy.txt',exact:true}).click();await row.getByRole('menuitem',{name:'Delete permanently',exact:true}).click();
  await page.getByRole('dialog',{name:'Delete permanently?'}).getByRole('button',{name:'Delete permanently',exact:true}).click();await expect(page.getByText('Your trash is empty.',{exact:true})).toBeVisible();
 }finally{c.wipe(fileKey);c.wipe(fixture.master);}
});
