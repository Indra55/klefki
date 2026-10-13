import {test,expect} from '@playwright/test';
test.beforeEach(async({page})=>{
 await page.addInitScript(()=>{
 const listeners:Record<string,Function>={};
 let state={config:{accountPath:'fixture-account.v1',storePath:'fixture.db',server:'',preferences:{theme:'dark',idleMinutes:5}},unlocked:true,generation:1,accountID:'fixture',recoveryOnly:false,sync:'Synced',pending:0,lastSync:''};
 const names=['Passport','Electricity bill, March','Rental agreement','Health insurance policy','Degree certificate'];
 const tags=['Identity','Money','Legal','Health','Education'];
 const items=names.map((name,i)=>({object:String(i),revision:String(i),name,type:'file',parent:null,tags:[tags[i]],favorite:false,size:250000+i*12000,conflict:false,trashed:false,trashRoot:false}));
 (window as any).runtime={EventsOn:(name:string,fn:Function)=>{listeners[name]=fn;return()=>delete listeners[name]}};
 (window as any).__emitKlefkiEvent=(event:any)=>listeners['klefki:event']?.(event);
 (window as any).go={main:{App:{AddFiles:async()=> 'upload-fixture',Cancel:async()=>{},State:async()=>structuredClone(state),List:async()=>({generation:state.generation,items}),Touch:async()=>{},Search:async(q:string)=>({generation:state.generation,hits:q==='nebula'?[{object:'0',revision:'0',name:'Passport',score:1}]:[]}),RebuildIndex:async()=> 'index-fixture',Lock:async()=>{state={...state,unlocked:false,generation:2};listeners['klefki:event']?.({generation:2,kind:'locked'})},SetPreferences:async(p:any)=>{state.config.preferences=p},ClosePreview:async()=>{}}}};
 });
});

for(const width of [320,390,600,800]){
 test(`upload has one success toast and no notification overlap at ${width}px`,async({page})=>{
  const errors:string[]=[];page.on('pageerror',e=>errors.push(e.message));
  await page.setViewportSize({width,height:700});
  await page.goto('/');await expect(page.getByText('Passport',{exact:true})).toBeVisible();
  await page.getByRole('button',{name:'Add files',exact:true}).click();
  await page.evaluate(()=>{
   const emit=(window as any).__emitKlefkiEvent;
   emit({generation:1,kind:'file-start',operation:'upload-fixture',name:'DJS23 Version 1 Final Year B. Tech Syllabus updated June 2026.pdf'});
   emit({generation:1,kind:'file-finished',operation:'upload-fixture',name:'DJS23 Version 1 Final Year B. Tech Syllabus updated June 2026.pdf'});
   emit({generation:1,kind:'index-start',operation:'upload-fixture'});
  });
  await expect(page.getByText('Indexing documents',{exact:true})).toBeVisible();
  await expect(page.getByRole('button',{name:'Cancel transfer'})).toBeVisible();
  await page.evaluate(()=>{
   const emit=(window as any).__emitKlefkiEvent;
   emit({generation:1,kind:'index-finished',operation:'upload-fixture',durationMs:3500,name:'4 documents indexed; 0 with names and tags only'});
   emit({generation:1,kind:'finished',operation:'upload-fixture'});
  });
  const toast=page.getByText('Search index ready in 3.5 s',{exact:true});
  await expect(toast).toBeVisible();
  await expect(page.getByText('Saved successfully',{exact:true})).toHaveCount(0);
  await expect(page.getByRole('complementary',{name:'File transfers'})).toHaveCount(0);
  // Another transfer can coexist with the toast without covering it.
  await page.evaluate(()=>(window as any).__emitKlefkiEvent({generation:1,kind:'file-start',operation:'other-transfer',name:'Another document with a long filename that needs to wrap on mobile.pdf'}));
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
test('settings navigation, preferences and narrow layouts with a fixture bridge',async({page})=>{
 const errors:string[]=[];page.on('pageerror',e=>errors.push(e.message));
 await page.goto('/');
 await page.getByRole('button',{name:'Settings',exact:true}).click();
 await expect(page.getByRole('heading',{name:'Settings',exact:true})).toBeFocused();
 await expect(page.getByRole('textbox',{name:'Search your documents'})).toHaveCount(0);
 for(const width of [1200,800,600,390,320]){
  await page.setViewportSize({width,height:820});
  await expect(page.getByLabel('Color theme')).toBeVisible();
  const control=(await page.getByLabel('Color theme').boundingBox())!;
  expect(control.x).toBeGreaterThanOrEqual(0);expect(control.x+control.width).toBeLessThanOrEqual(width);
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBeTruthy();
  await page.screenshot({path:`test-results/settings-${width}.png`,fullPage:true});
 }
 await page.getByLabel('Color theme').selectOption('light');
 await expect(page.locator('html')).toHaveAttribute('data-theme','light');
 await page.getByLabel('Document spacing').selectOption('compact');
 await expect(page.getByLabel('Document spacing')).toHaveValue('compact');
 await page.getByLabel('Default sort order').selectOption('size');
 await expect(page.getByLabel('Default sort order')).toHaveValue('size');
 await page.getByRole('button',{name:'Privacy & lock'}).click();
 await page.getByLabel('Lock after inactivity').selectOption('15');
 await expect(page.getByLabel('Lock after inactivity')).toHaveValue('15');
 await page.getByRole('button',{name:'Sync & devices'}).click();
 await expect(page.getByLabel('Server address')).toBeVisible();
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBeTruthy();
 await page.screenshot({path:'test-results/settings-sync-small.png',fullPage:true});
 await page.getByRole('button',{name:'Search',exact:true}).click();
 await page.getByRole('button',{name:'Rebuild search index',exact:true}).click();
 await expect(page.getByRole('button',{name:'Indexing documents…'})).toBeDisabled();
 await page.evaluate(()=>(window as any).__emitKlefkiEvent({generation:1,kind:'finished',operation:'index-fixture'}));
 await expect(page.getByRole('button',{name:'Rebuild search index',exact:true})).toBeEnabled();
 await page.getByRole('button',{name:'Back to library'}).click();
 await expect(page.getByRole('button',{name:'Settings',exact:true})).toBeFocused();
 await expect(page.getByLabel('Sort documents')).toHaveValue('size');
 await expect(page.locator('.library')).toHaveAttribute('data-density','compact');
 await expect(page.getByRole('button',{name:'New folder',exact:true})).toBeVisible();
 await page.getByRole('button',{name:'Settings',exact:true}).click();
 await expect(page.getByLabel('Color theme')).toHaveValue('light');
 await expect(page.getByLabel('Document spacing')).toHaveValue('compact');
 await page.getByRole('button',{name:'Lock',exact:true}).click();
 await expect(page.getByText('Unlock Klefki.')).toBeVisible();
 await expect(page.getByRole('heading',{name:'Settings',exact:true})).toHaveCount(0);
 expect(errors).toEqual([]);
});
test('reference layout, themes, search and lock with an explicit fixture bridge',async({page})=>{
 const errors:string[]=[];page.on('pageerror',e=>errors.push(e.message));
 await page.goto('/');await expect(page.getByText('Passport',{exact:true})).toBeVisible();await page.screenshot({path:'test-results/library-dark.png',fullPage:true});
 await page.getByLabel('Search your documents').fill('rental');await expect(page.getByText('Rental agreement',{exact:true})).toBeVisible();await expect(page.getByText('Passport',{exact:true})).toHaveCount(0);await page.getByLabel('Clear search').click();
 await page.getByLabel('Search your documents').fill('nebula');await expect(page.getByText('Passport',{exact:true})).toBeVisible();await expect(page.getByText('Rental agreement',{exact:true})).toHaveCount(0);await page.getByLabel('Clear search').click();
 await page.getByRole('button',{name:'Settings',exact:true}).click();await page.getByLabel('Color theme').selectOption('light');await expect(page.locator('html')).toHaveAttribute('data-theme','light');await page.getByRole('button',{name:'Back to library'}).click();await page.screenshot({path:'test-results/library-light.png',fullPage:true});
 await page.setViewportSize({width:800,height:600});await page.screenshot({path:'test-results/library-small.png',fullPage:true});expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBeTruthy();
 await page.getByRole('button',{name:'Lock',exact:true}).click();await expect(page.getByText('Unlock Klefki.')).toBeVisible();await expect(page.getByText('Passport',{exact:true})).toHaveCount(0);await page.screenshot({path:'test-results/locked.png',fullPage:true});expect(errors).toEqual([]);
});

test('sync menu keeps progress visible, paginates history and fits narrow screens with a fixture bridge',async({page})=>{
 await page.addInitScript(()=>{
  const app=(window as any).go.main.App,original=app.State,self='11111111111111111111111111111111';
  app.State=async()=>{const state=await original();return {...state,config:{...state.config,server:'https://sync.test'},syncDevice:self}};
  app.RefreshSync=async()=>{
   const emit=(window as any).__emitKlefkiEvent;
   emit({generation:1,kind:'sync-status',syncInfo:{phase:'connecting',running:true,lastContact:0,lastDeviceCheck:0,statusAvailable:false}});
   setTimeout(()=>emit({generation:1,kind:'sync-status',syncInfo:{phase:'synced',running:false,lastContact:Date.now(),lastDeviceCheck:0,statusAvailable:false}}),500);
  };
 });
 await page.goto('/');await expect(page.getByText('Passport',{exact:true})).toBeVisible();
 const trigger=page.getByRole('button',{name:'Sync status',exact:true});
 await expect(trigger).toHaveAttribute('data-state','synced');
 await page.evaluate(()=>{
  for(let i=0;i<12;i++)(window as any).__emitKlefkiEvent({generation:1,kind:'sync-status',syncInfo:{phase:'synced',running:false,lastContact:0,lastDeviceCheck:0,statusAvailable:false,progress:{phase:'complete',revision:String(i),name:`Transfer ${i}`,direction:'upload',doneBytes:10,totalBytes:10,reusedBytes:0,chunk:1,chunks:1,updatedAt:Date.now()}}});
 });
 await trigger.click();const panel=page.getByRole('dialog',{name:'Sync activity'});
 await expect(panel.locator('.sync-history li')).toHaveCount(5);
 await panel.getByRole('button',{name:'Next',exact:true}).click();
 await expect(panel.getByText('Page 2 of 3',{exact:true})).toBeVisible();
 await panel.getByRole('button',{name:'Refresh sync',exact:true}).click();
 await expect(panel.getByRole('button',{name:'Refresh sync',exact:true})).toBeDisabled();
 await expect(panel.getByText('Checking connection',{exact:true}).first()).toBeVisible();
 await expect(panel.getByRole('button',{name:'Refresh sync',exact:true})).toBeEnabled();
 await expect(panel).toBeVisible();await expect(panel.getByText('Page 2 of 3',{exact:true})).toBeVisible();
 for(const width of [320,390,600,800,1200]){
  await page.setViewportSize({width,height:740});const rect=(await panel.boundingBox())!;
  expect(rect.x).toBeGreaterThanOrEqual(0);expect(rect.x+rect.width).toBeLessThanOrEqual(width);
  expect(rect.y).toBeGreaterThanOrEqual(0);expect(rect.y+rect.height).toBeLessThanOrEqual(740);
  if(width<=600){expect(Math.abs(rect.x+rect.width/2-width/2)).toBeLessThanOrEqual(1);expect(Math.abs(rect.y+rect.height/2-370)).toBeLessThanOrEqual(1)}
  await page.screenshot({path:`test-results/sync-menu-${width}.png`});
 }
 await panel.getByRole('button',{name:'Close sync activity',exact:true}).click();await trigger.click();
 await expect(panel.getByText('Page 1 of 3',{exact:true})).toBeVisible();
});
