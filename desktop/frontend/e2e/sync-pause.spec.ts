import {test,expect} from '@playwright/test';

test('desktop pause controls agree in settings and activity across narrow layouts',async({page})=>{
 await page.addInitScript(()=>{
  const info={phase:'pending',running:false,paused:false,lastContact:0,lastDeviceCheck:0,statusAvailable:false};
  const state={config:{accountPath:'public-fixture',storePath:'fixture.db',server:'https://fixture.example',preferences:{theme:'dark',idleMinutes:5}},unlocked:true,generation:1,accountID:'fixture',recoveryOnly:false,sync:'Pending',pending:3,lastSync:'',syncInfo:info};
  (window as any).runtime={EventsOn:()=>()=>{}};
  (window as any).go={main:{App:{State:async()=>structuredClone(state),List:async()=>({generation:1,items:[]}),Touch:async()=>{},SetSyncPanel:async()=>{},SetSyncPaused:async(paused:boolean)=>{info.paused=paused;},SyncNow:async()=>{},RefreshSync:async()=>{}}}};
 });
 await page.goto('/');
 await page.getByRole('button',{name:'Settings',exact:true}).click();
 await page.getByRole('button',{name:'Sync & devices',exact:true}).click();
 await page.getByRole('button',{name:'Pause sync',exact:true}).click();
 await expect(page.getByRole('button',{name:'Resume sync',exact:true})).toBeVisible();
 await expect(page.getByRole('button',{name:'Sync now',exact:true})).toBeDisabled();
 for(const width of [1200,800,600,390,320]){
  await page.setViewportSize({width,height:820});
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
  const box=(await page.getByRole('button',{name:'Resume sync',exact:true}).boundingBox())!;
  expect(box.x).toBeGreaterThanOrEqual(0);expect(box.x+box.width).toBeLessThanOrEqual(width);
  if(width===390)await page.screenshot({path:'/tmp/klefki-desktop-sync-pause.png',fullPage:true});
 }
 await page.getByRole('button',{name:'Back to library',exact:true}).click();
 await expect(page.getByRole('region',{name:'Sync paused'})).toBeVisible();
 await page.getByRole('button',{name:'Sync status',exact:true}).click();
 const panel=page.getByRole('dialog',{name:'Sync activity'});
 await expect(panel.getByRole('button',{name:'Refresh sync',exact:true})).toBeDisabled();
 await expect(panel).toContainText('3 changes waiting to upload.');
 await panel.getByRole('button',{name:'Resume sync',exact:true}).click();
 await expect(panel.getByRole('button',{name:'Pause sync',exact:true})).toBeVisible();
 await expect(panel.getByRole('button',{name:'Refresh sync',exact:true})).toBeEnabled();
 await expect(page.getByRole('button',{name:'Sync status',exact:true})).toHaveAttribute('data-state','pending');
});
