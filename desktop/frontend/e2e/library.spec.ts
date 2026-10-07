import {test,expect} from '@playwright/test';
test.beforeEach(async({page})=>{
 await page.addInitScript(()=>{
 const listeners:Record<string,Function>={};
 let state={config:{accountPath:'fixture-account.v1',storePath:'fixture.db',server:'',preferences:{theme:'dark',idleMinutes:5}},unlocked:true,generation:1,accountID:'fixture',recoveryOnly:false,sync:'Synced',pending:0,lastSync:''};
 const names=['Passport','Electricity bill, March','Rental agreement','Health insurance policy','Degree certificate'];
 const tags=['Identity','Money','Legal','Health','Education'];
 const items=names.map((name,i)=>({object:String(i),revision:String(i),name,type:'file',parent:null,tags:[tags[i]],favorite:false,size:250000+i*12000,conflict:false,trashed:false,trashRoot:false}));
 (window as any).runtime={EventsOn:(name:string,fn:Function)=>{listeners[name]=fn;return()=>delete listeners[name]}};
 (window as any).__emitVaultEvent=(event:any)=>listeners['vault:event']?.(event);
 (window as any).go={main:{App:{AddFiles:async()=> 'upload-fixture',Cancel:async()=>{},State:async()=>structuredClone(state),List:async()=>({generation:state.generation,items}),Touch:async()=>{},Search:async(q:string)=>({generation:state.generation,hits:q==='nebula'?[{object:'0',revision:'0',name:'Passport',score:1}]:[]}),RebuildIndex:async()=> 'index-fixture',Lock:async()=>{state={...state,unlocked:false,generation:2};listeners['vault:event']?.({generation:2,kind:'locked'})},SetPreferences:async(p:any)=>{state.config.preferences=p},ClosePreview:async()=>{}}}};
 });
});

for(const width of [320,390,600,800]){
 test(`upload has one success toast and no notification overlap at ${width}px`,async({page})=>{
  const errors:string[]=[];page.on('pageerror',e=>errors.push(e.message));
  await page.setViewportSize({width,height:700});
  await page.goto('/');await expect(page.getByText('Passport',{exact:true})).toBeVisible();
  await page.getByRole('button',{name:'Add files',exact:true}).click();
  await page.evaluate(()=>{
   const emit=(window as any).__emitVaultEvent;
   emit({generation:1,kind:'file-start',operation:'upload-fixture',name:'DJS23 Version 1 Final Year B. Tech Syllabus updated June 2026.pdf'});
   emit({generation:1,kind:'file-finished',operation:'upload-fixture',name:'DJS23 Version 1 Final Year B. Tech Syllabus updated June 2026.pdf'});
   emit({generation:1,kind:'index-start',operation:'upload-fixture'});
  });
  await expect(page.getByText('Indexing documents',{exact:true})).toBeVisible();
  await expect(page.getByRole('button',{name:'Cancel transfer'})).toBeVisible();
  await page.evaluate(()=>{
   const emit=(window as any).__emitVaultEvent;
   emit({generation:1,kind:'index-finished',operation:'upload-fixture',durationMs:3500,name:'4 documents indexed; 0 with names and tags only'});
   emit({generation:1,kind:'finished',operation:'upload-fixture'});
  });
  const toast=page.getByText('Search index ready in 3.5 s',{exact:true});
  await expect(toast).toBeVisible();
  await expect(page.getByText('Saved successfully',{exact:true})).toHaveCount(0);
  await expect(page.getByRole('complementary',{name:'File transfers'})).toHaveCount(0);
  // Another transfer can coexist with the toast without covering it.
  await page.evaluate(()=>(window as any).__emitVaultEvent({generation:1,kind:'file-start',operation:'other-transfer',name:'Another document with a long filename that needs to wrap on mobile.pdf'}));
  const transfers=page.getByRole('complementary',{name:'File transfers'});
  await expect(transfers).toBeVisible();
  const transferBox=(await transfers.boundingBox())!;
  const toastBox=(await page.locator('.unlock-toast').boundingBox())!;
  expect(transferBox.y+transferBox.height).toBeLessThanOrEqual(toastBox.y);
  for(const box of [transferBox,toastBox]){
   expect(box.x).toBeGreaterThanOrEqual(0);
   expect(box.x+box.width).toBeLessThanOrEqual(width);
   expect(box.y+box.height).toBeLessThanOrEqual(700);
  }
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBeTruthy();
  await page.screenshot({path:`test-results/notifications-${width}.png`,fullPage:true});
  expect(errors).toEqual([]);
 });
}
test('reference layout, themes, search and lock with an explicit fixture bridge',async({page})=>{
 const errors:string[]=[];page.on('pageerror',e=>errors.push(e.message));
 await page.goto('/');await expect(page.getByText('Passport',{exact:true})).toBeVisible();await page.screenshot({path:'test-results/library-dark.png',fullPage:true});
 await page.getByLabel('Search your documents').fill('rental');await expect(page.getByText('Rental agreement',{exact:true})).toBeVisible();await expect(page.getByText('Passport',{exact:true})).toHaveCount(0);await page.getByLabel('Clear search').click();
 await page.getByLabel('Search your documents').fill('nebula');await expect(page.getByText('Passport',{exact:true})).toBeVisible();await expect(page.getByText('Rental agreement',{exact:true})).toHaveCount(0);await page.getByLabel('Clear search').click();
 await page.getByRole('button',{name:'All documents'}).click();await page.getByRole('menuitem',{name:'Settings',exact:true}).click();await page.getByLabel('Appearance').selectOption('light');await expect(page.locator('html')).toHaveAttribute('data-theme','light');await page.getByLabel('Close',{exact:true}).click();await page.screenshot({path:'test-results/library-light.png',fullPage:true});
 await page.setViewportSize({width:800,height:600});await page.screenshot({path:'test-results/library-small.png',fullPage:true});expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBeTruthy();
 await page.getByRole('button',{name:'Lock',exact:true}).click();await expect(page.getByText('Unlock your vault.')).toBeVisible();await expect(page.getByText('Passport',{exact:true})).toHaveCount(0);await page.screenshot({path:'test-results/locked.png',fullPage:true});expect(errors).toEqual([]);
});
