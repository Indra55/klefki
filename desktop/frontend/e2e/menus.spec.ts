import {test,expect} from '@playwright/test';

test('desktop document menu flips and scrolls within short and narrow viewports',async({page})=>{
 await page.addInitScript(()=>{
  const state={config:{accountPath:'public-fixture',storePath:'fixture.db',server:'',preferences:{theme:'dark',idleMinutes:5}},unlocked:true,generation:1,accountID:'fixture',recoveryOnly:false,sync:'Local only',pending:0,lastSync:''};
  const item={name:'Edge document',object:'fixture-object',revision:'fixture-revision',type:'file',parent:null,tags:[],favorite:false,size:5,conflict:false,trashed:false,trashRoot:false};
  (window as any).runtime={EventsOn:()=>()=>{}};
  (window as any).go={main:{App:{State:async()=>state,List:async()=>({generation:1,items:[item]}),Touch:async()=>{}}}};
 });
 await page.goto('/');const trigger=page.getByRole('button',{name:'Actions for Edge document',exact:true});
 for(const size of [{width:320,height:480},{width:390,height:600},{width:600,height:560},{width:1280,height:720},{width:568,height:180}]){
  await page.setViewportSize(size);
  await page.locator('.library').evaluate(el=>(el as HTMLElement).style.paddingBottom='1000px');
  for(const top of [20,size.height-52]){
   await trigger.evaluate((el,top)=>window.scrollBy(0,el.getBoundingClientRect().top-top),top);
   await trigger.click();const menu=page.getByRole('menu');await expect(menu).toBeVisible();
   await expect.poll(async()=>{const box=(await menu.boundingBox())!;return box.x>=11&&box.y>=11&&box.x+box.width<=size.width-11&&box.y+box.height<=size.height-11}).toBe(true);
   await page.getByRole('menuitem',{name:'Move to Trash',exact:true}).focus();
   await expect(page.getByRole('menuitem',{name:'Move to Trash',exact:true})).toBeInViewport();
   await page.keyboard.press('Escape');await expect(trigger).toBeFocused();
  }
 }
});
