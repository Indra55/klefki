import {test,expect,type Page} from '@playwright/test';
import {upload} from './ui.ts';

async function inViewport(page:Page){
 const menu=page.getByRole('menu');await expect(menu).toBeVisible();
 await expect.poll(async()=>{const box=(await menu.boundingBox())!,v=await page.evaluate(()=>({left:visualViewport?.offsetLeft||0,top:visualViewport?.offsetTop||0,width:visualViewport?.width||innerWidth,height:visualViewport?.height||innerHeight}));return box.x>=v.left+11&&box.y>=v.top+11&&box.x+box.width<=v.left+v.width-11&&box.y+box.height<=v.top+v.height-11}).toBe(true);
}

test('document and view menus stay inside screen edges, resize and scroll with keyboard access',async({page})=>{
 await page.goto('/');
 await page.getByLabel('Password',{exact:true}).fill('public menu fixture password');
 await page.getByLabel('Confirm password').fill('public menu fixture password');
 await page.getByRole('button',{name:'Create Klefki',exact:true}).click();
 await page.getByRole('button',{name:'I have saved it',exact:true}).click();
 await upload(page,{name:'edge.txt',mimeType:'text/plain',buffer:Buffer.from('public menu fixture')});
 const trigger=page.getByRole('button',{name:'Actions for edge.txt',exact:true});
 for(const size of [{width:320,height:480},{width:390,height:600},{width:600,height:560},{width:1280,height:720}]){
  await page.setViewportSize(size);
  // Leave enough document height to put the same real trigger at either edge.
  await page.locator('.library').evaluate(el=>(el as HTMLElement).style.paddingBottom='1000px');
  for(const top of [20,size.height-52]){
   await trigger.evaluate((el,top)=>window.scrollBy(0,el.getBoundingClientRect().top-top),top);
   await trigger.click();await inViewport(page);
   await page.getByRole('menuitem',{name:'Move to Trash',exact:true}).focus();
   await page.keyboard.press('Escape');await expect(trigger).toBeFocused();
   await expect(page.getByRole('menu')).toHaveCount(0);
  }
 }
 await page.setViewportSize({width:390,height:600});await trigger.scrollIntoViewIfNeeded();await trigger.click();
 await page.setViewportSize({width:568,height:180});await inViewport(page);
 // The short viewport scrolls the menu so its last action remains reachable.
 await page.getByRole('menuitem',{name:'Move to Trash',exact:true}).focus();
 await expect(page.getByRole('menuitem',{name:'Move to Trash',exact:true})).toBeInViewport();
 await page.keyboard.press('Escape');await expect(trigger).toBeFocused();
 await page.setViewportSize({width:390,height:600});await trigger.scrollIntoViewIfNeeded();await trigger.focus();
 await page.keyboard.press('ArrowDown');await inViewport(page);
 await expect(page.getByRole('menuitem',{name:'Export',exact:true})).toBeFocused();
 await page.evaluate(()=>window.scrollBy(0,24));await inViewport(page);
 await page.keyboard.press('Escape');
 const cdp=await page.context().newCDPSession(page);
 await cdp.send('Emulation.setPageScaleFactor',{pageScaleFactor:2});
 await expect.poll(()=>page.evaluate(()=>visualViewport?.scale)).toBe(2);
 await trigger.evaluate(el=>(el as HTMLElement).click());await inViewport(page);
 await page.screenshot({path:'/tmp/klefki-menu-zoomed.png'});
 await cdp.send('Emulation.setPageScaleFactor',{pageScaleFactor:1});await inViewport(page);
 await page.keyboard.press('Escape');
 await page.getByRole('button',{name:'View',exact:true}).scrollIntoViewIfNeeded();
 await page.getByRole('button',{name:'View',exact:true}).click();await inViewport(page);
 await page.getByRole('menuitem',{name:'Favorites',exact:true}).click();
 await expect(page.getByRole('menu')).toHaveCount(0);
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
});
