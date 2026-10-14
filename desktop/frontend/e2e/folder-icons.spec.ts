import {test,expect} from '@playwright/test';

test('desktop folder picker loads every emoji and keeps keyboard selection within narrow viewports',async({page})=>{
 await page.addInitScript(()=>{
  const items:any[]=[];
  const state={config:{accountPath:'public-fixture',storePath:'fixture.db',server:'',preferences:{theme:'dark',idleMinutes:5}},unlocked:true,generation:1,accountID:'fixture',recoveryOnly:false,sync:'Local only',pending:0,lastSync:''};
  (window as any).runtime={EventsOn:()=>()=>{}};
  (window as any).go={main:{App:{State:async()=>state,List:async()=>({generation:1,items}),Touch:async()=>{},FolderWithIcon:async(name:string,parent:string|null,folder_icon:string)=>{items.push({name,parent,folder_icon,object:'folder',revision:'folder-rev',type:'folder',tags:[],favorite:false,size:0,conflict:false,trashed:false,trashRoot:false});return 'folder-rev'},Edit:async(revision:string,edit:any)=>{Object.assign(items.find(i=>i.revision===revision),edit);return revision}}}};
 });
 await page.goto('/');await page.getByRole('button',{name:'New folder',exact:true}).click();
 await page.getByLabel('Folder name').fill('Study records');
 const picker=page.getByRole('button',{name:'Choose folder icon',exact:true});
 await picker.focus();await page.keyboard.press('Enter');
 const grid=page.getByRole('group',{name:'Folder icons'});
 await expect.poll(()=>grid.locator('img').evaluateAll(images=>images.length===32&&images.every(img=>(img as HTMLImageElement).complete&&(img as HTMLImageElement).naturalWidth>0))).toBe(true);
 for(const width of [1200,390,320]){
  await page.setViewportSize({width,height:820});
  const box=(await grid.boundingBox())!;expect(box.x).toBeGreaterThanOrEqual(0);expect(box.x+box.width).toBeLessThanOrEqual(width);
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
  if(width===390)await page.screenshot({path:'/tmp/klefki-desktop-folder-picker.png'});
 }
 await page.getByRole('button',{name:'Graduation cap',exact:true}).focus();await page.keyboard.press('Enter');
 await expect(picker).toBeFocused();
 await page.getByRole('button',{name:'Create folder',exact:true}).click();
 const row=page.locator('article').filter({has:page.getByText('Study records',{exact:true})});
 await expect(row.locator('img')).toHaveAttribute('src',/1f393/);
 await row.getByRole('button',{name:'Actions for Study records',exact:true}).click();
 await page.getByRole('menuitem',{name:'Edit details & move',exact:true}).click();
 await picker.click();await page.keyboard.press('Escape');
 await expect(page.getByRole('dialog',{name:'Document details'})).toBeVisible();await expect(grid).not.toBeVisible();
 await picker.click();await page.getByRole('button',{name:'Briefcase',exact:true}).click();
 await page.getByRole('button',{name:'Save changes',exact:true}).click();
 await expect(row.locator('img')).toHaveAttribute('src',/1f4bc/);
 await row.getByText('Study records',{exact:true}).click();
 await expect(page.getByRole('navigation',{name:'Folder breadcrumb'}).locator('img')).toHaveAttribute('src',/1f4bc/);
});
