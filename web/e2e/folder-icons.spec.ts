import {test,expect,type Page} from '@playwright/test';
import * as c from '../src/crypto.ts';
import {backToLibrary,documentAction,openSettings,syncNow} from './ui.ts';

const password='public folder emoji fixture password';
async function open(page:Page,account:c.Account){
 await page.goto('/');
 await page.getByLabel('Open an encrypted account file').setInputFiles({name:'account.klefki',mimeType:'application/octet-stream',buffer:Buffer.from(c.accountBytes(account))});
 await page.getByLabel('Password',{exact:true}).fill(password);
 await page.getByRole('button',{name:'Unlock Klefki',exact:true}).click();
 await expect(page.locator('.library')).toBeVisible();
}
const folder=(page:Page,name='Education')=>page.getByRole('button',{name:'Open '+name,exact:true});
const image=(page:Page,name='Education')=>folder(page,name).locator('img.folder-emoji');

test('folder emoji survives encrypted sync, favorite, trash, offline reload and reset',async({page,browser,context})=>{
 await c.ready;const created=c.createAccount(password),other=await browser.newContext(),b=await other.newPage();
 try{
  await page.setViewportSize({width:390,height:844});
  await open(page,created.account);await openSettings(page,'Sync & devices');
  await page.getByRole('button',{name:'Register this store on server',exact:true}).click();
  await expect(page.getByRole('status')).toContainText('Synced.');
  await backToLibrary(page);await expect(page.locator('.library')).toBeVisible();
  await page.getByRole('button',{name:'New folder',exact:true}).click();
  await page.getByLabel('Folder name').fill('Education');
  const picker=page.getByRole('button',{name:'Choose folder icon',exact:true});
  await picker.focus();await page.keyboard.press('Enter');
  await expect(page.getByRole('group',{name:'Folder icons'})).toBeVisible();
  await expect.poll(()=>page.getByRole('group',{name:'Folder icons'}).locator('img').evaluateAll(images=>images.every(img=>(img as HTMLImageElement).complete&&(img as HTMLImageElement).naturalWidth>0))).toBe(true);
  for(const width of [320,390,600]){
   await page.setViewportSize({width,height:844});
   expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
   const box=(await page.getByRole('group',{name:'Folder icons'}).boundingBox())!;
   expect(box.x).toBeGreaterThanOrEqual(0);expect(box.x+box.width).toBeLessThanOrEqual(width);
   if(width===390)await page.screenshot({path:'/tmp/klefki-folder-icons-mobile.png'});
  }
  await page.getByRole('button',{name:'Graduation cap',exact:true}).focus();await page.keyboard.press('Enter');
  await expect(picker).toBeFocused();
  await page.getByRole('button',{name:'Create folder',exact:true}).click();
  await expect(image(page)).toHaveAttribute('src',/1f393/);
  await page.screenshot({path:'/tmp/klefki-folder-icons-library.png'});
  await expect.poll(()=>image(page).evaluate((img:HTMLImageElement)=>img.complete&&img.naturalWidth>0)).toBe(true);
  await expect(page.getByRole('button',{name:'Sync status',exact:true})).toHaveAttribute('data-state','synced');
  await open(b,created.account);await expect(image(b)).toHaveAttribute('src',/1f393/);
  await documentAction(b,'Education','Edit details & move');
  await b.getByRole('button',{name:'Choose folder icon',exact:true}).click();
  await expect(b.getByRole('button',{name:'Graduation cap',exact:true})).toHaveAttribute('aria-pressed','true');
  await b.getByRole('button',{name:'Briefcase',exact:true}).click();
  await b.getByRole('button',{name:'Save changes',exact:true}).click();
  await expect(image(b)).toHaveAttribute('src',/1f4bc/);
  await b.getByRole('button',{name:'Favorite Education',exact:true}).click();
  await expect(b.getByRole('button',{name:'Unfavorite Education',exact:true})).toBeVisible();
  await expect(b.getByRole('button',{name:'Sync status',exact:true})).toHaveAttribute('data-state',/^(synced|waiting)$/);
  await syncNow(page);await expect(image(page)).toHaveAttribute('src',/1f4bc/);
  await documentAction(page,'Education','Move to Trash');
  await page.getByRole('button',{name:'View',exact:true}).click();await page.getByRole('menuitem',{name:'Trash',exact:true}).click();
  await expect(image(page)).toHaveAttribute('src',/1f4bc/);
  await page.getByRole('button',{name:'Restore',exact:true}).click();
  await page.getByRole('navigation',{name:'Folder breadcrumb'}).getByRole('button',{name:'All',exact:true}).click();
  await expect(image(page)).toHaveAttribute('src',/1f4bc/);
  await page.evaluate(async()=>{await navigator.serviceWorker.ready});
  await expect.poll(()=>page.evaluate(()=>!!navigator.serviceWorker.controller)).toBe(true);
  await context.setOffline(true);await page.reload();
  await page.getByLabel('Password',{exact:true}).fill(password);await page.getByRole('button',{name:'Unlock Klefki',exact:true}).click();
  await expect.poll(()=>image(page).evaluate((img:HTMLImageElement)=>img.complete&&img.naturalWidth>0)).toBe(true);
  await expect(image(page)).toHaveAttribute('src',/1f4bc/);
  await folder(page).click();
  await expect(page.getByRole('navigation',{name:'Folder breadcrumb'}).locator('img')).toHaveAttribute('src',/1f4bc/);
  await page.getByRole('navigation',{name:'Folder breadcrumb'}).getByRole('button',{name:'All',exact:true}).click();
  await documentAction(page,'Education','Edit details & move');
  await picker.click();
  await expect.poll(()=>page.getByRole('group',{name:'Folder icons'}).locator('img').evaluateAll(images=>images.every(img=>(img as HTMLImageElement).complete&&(img as HTMLImageElement).naturalWidth>0))).toBe(true);
  await page.getByRole('button',{name:'Default folder',exact:true}).click();
  await page.getByRole('button',{name:'Save changes',exact:true}).click();
  await expect(image(page)).toHaveAttribute('src',/1f4c1/);
 }finally{await context.setOffline(false);await other.close();c.wipe(created.master);}
});
