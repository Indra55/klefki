import {beforeEach,afterEach,describe,it,expect,vi} from 'vitest';
import {render,screen,waitFor,act,cleanup} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import App from './App';
import {idleSync,receipt,presence,syncIndicator,TransferRate,type SyncProgress} from '../../../ui/sync-status';
import {SyncStatus} from './SyncStatus';
import type {State,KlefkiEvent} from './api';

const self='11111111111111111111111111111111',phone='22222222222222222222222222222222';
let state:State,emit:(event:KlefkiEvent)=>void;
const p:SyncProgress={phase:'uploading',revision:'33333333333333333333333333333333',name:'private-report.pdf',direction:'upload',doneBytes:4194304,totalBytes:8388608,reusedBytes:0,chunk:1,chunks:2,updatedAt:Date.now()};
beforeEach(()=>{
  state={config:{accountPath:'account',storePath:'store',server:'https://sync.test',preferences:{theme:'dark',idleMinutes:5}},unlocked:true,generation:1,accountID:'account',recoveryOnly:false,sync:'Synced',pending:1,lastSync:'',syncDevice:self,syncDevices:[self,phone],deviceAliases:{[phone]:'My phone'},syncInfo:{...idleSync(),phase:'synced'}};
  window.matchMedia=vi.fn().mockReturnValue({matches:false,addEventListener:vi.fn(),removeEventListener:vi.fn()});
  window.runtime={EventsOn:(name,cb)=>{if(name==='klefki:event')emit=cb;return()=>{}}};
  window.go={main:{App:{State:vi.fn(async()=>state),List:vi.fn(()=>new Promise(()=>{})),Touch:vi.fn(),SyncNow:vi.fn(async()=>{}),RefreshSync:vi.fn(async()=>{}),SetSyncPanel:vi.fn(async()=>{}),SetDeviceAlias:vi.fn(async()=>{}),ClosePreview:vi.fn()} as any}};
});
afterEach(()=>{cleanup();vi.restoreAllMocks()});

describe('sync visibility',()=>{
  it('updates progress while the serialized library read is still blocked',async()=>{
    render(<App/>);await screen.findByRole('button',{name:'Sync status'});
    act(()=>emit({generation:1,kind:'sync-status',syncInfo:{...idleSync(),phase:'uploading',running:true,progress:p}}));
    expect(screen.getByRole('progressbar',{name:'Document sync progress'})).toHaveAttribute('value','4194304');
    expect(screen.getByText(/private-report.pdf.*4.0 MB of 8.0 MB/)).toBeVisible();
    expect(window.go!.main.App.List).toHaveBeenCalledTimes(1);
    act(()=>emit({generation:1,kind:'sync-status',syncInfo:{...idleSync(),phase:'committing',running:true,progress:{...p,phase:'committing',doneBytes:8388608}}}));
    expect(screen.getByRole('button',{name:'Sync status'})).toHaveTextContent('Saving changes');
    expect(screen.getByRole('button',{name:'Sync status'})).not.toHaveTextContent('Up to date');
  });
  it('distinguishes server completion from device receipt and keeps nicknames local',async()=>{
    state.pending=0;
    const now=Date.now();state.syncInfo={...idleSync(),phase:'synced',statusAvailable:true,lastDeviceCheck:now,peers:{epoch:self,latestSequence:3,devices:[{device:phone,clientKind:'pwa',phase:'idle',doneBytes:0,totalBytes:0,lastSeen:now,sequence:1,outstanding:2}]}};
    render(<App/>);const user=userEvent.setup();await user.click(await screen.findByRole('button',{name:'Sync status'}));
    expect(screen.getByText('Saved on server; waiting for My phone.')).toBeVisible();
    expect(screen.getByText('2 changes waiting')).toBeVisible();
    const input=screen.getByRole('textbox',{name:'Nickname for device 22222222'});await user.clear(input);await user.type(input,'Work phone');await user.click(screen.getByRole('button',{name:'Save'}));
    await waitFor(()=>expect(window.go!.main.App.SetDeviceAlias).toHaveBeenCalledWith(phone,'Work phone'));
  });
  it('clears private activity on lock and ignores old generation progress',async()=>{
    render(<App/>);await screen.findByRole('button',{name:'Sync status'});
    act(()=>emit({generation:1,kind:'sync-status',syncInfo:{...idleSync(),phase:'uploading',running:true,progress:{...p,updatedAt:Date.now()-12000}}}));
    expect(screen.getByText(/Taking longer than usual/)).toBeVisible();
    state={...state,unlocked:false,generation:2};act(()=>emit({generation:2,kind:'locked'}));
    await screen.findByRole('button',{name:'Unlock Klefki'});
    act(()=>emit({generation:1,kind:'sync-status',syncInfo:{...idleSync(),phase:'uploading',running:true,progress:p}}));
    expect(screen.queryByText(/private-report.pdf/)).not.toBeInTheDocument();
  });
  it('expires presence and hides receipt certainty after failed polling',()=>{
    const now=Date.now(),d={device:phone,clientKind:'pwa',phase:'idle',doneBytes:0,totalBytes:0,lastSeen:now-46000,sequence:3,outstanding:0};
    expect(presence(d,now,now)).toBe('Not recently seen');expect(receipt(d,false)).toBe('Status unknown');
    const rate=new TransferRate();rate.update({...p,doneBytes:0,chunk:0,updatedAt:1000});rate.update({...p,updatedAt:4000});
    expect(rate.update({...p,chunk:2,doneBytes:8388608,updatedAt:8000})).toContain('/s');
    expect(rate.update({...p,phase:'retrying',updatedAt:9000})).toBe('');
  });
  it('prioritizes connection failures, local work and fresh incoming changes',()=>{
    const now=Date.now(),info={...idleSync(),phase:'synced',statusAvailable:true,lastDeviceCheck:now,peers:{epoch:self,latestSequence:3,devices:[{device:self,clientKind:'desktop',phase:'idle',doneBytes:0,totalBytes:0,lastSeen:now,sequence:1,outstanding:2}]}};
    expect(syncIndicator(info,self,{},0,now)).toEqual({state:'waiting',label:'Changes waiting to download'});
    expect(syncIndicator(info,self,{},1,now).state).toBe('pending');
    expect(syncIndicator({...info,running:true},self,{},1,now).state).toBe('running');
    expect(syncIndicator({...info,phase:'offline',running:true},self,{},1,now)).toEqual({state:'neutral',label:'Network unavailable'});
    for(const phase of ['unreachable','timeout','auth','full','error'])expect(syncIndicator({...info,phase,running:true},self,{},1,now).state).toBe('error');
    expect(syncIndicator(info,self,{},0,now+45000).state).toBe('synced');
    expect(syncIndicator({...info,phase:'waiting',waitingFor:'source'},self,{},0,now).label).toBe('Waiting for another device to supply changes');
    expect(syncIndicator({...idleSync(),phase:'unknown'},self,{},0,now).state).toBe('neutral');
  });
  it('refreshes without closing the menu and shows dispatch errors there',async()=>{
    const user=userEvent.setup(),onOpen=vi.fn();
    window.go!.main.App.RefreshSync=vi.fn().mockRejectedValue(new Error('network'));
    render(<SyncStatus state={state} activity={[]} open onOpen={onOpen} onSettings={vi.fn()} onRefresh={vi.fn()}/>);
    await user.click(screen.getByRole('button',{name:'Refresh sync'}));
    expect(window.go!.main.App.RefreshSync).toHaveBeenCalledTimes(1);
    expect(onOpen).not.toHaveBeenCalled();
    expect(await screen.findByRole('alert')).toHaveTextContent('Could not start sync.');
    expect(screen.getByRole('dialog',{name:'Sync activity'})).toBeVisible();
  });
  it('paginates activity, preserves the page on ticks, clamps on removal and resets on reopen',async()=>{
    const user=userEvent.setup(),activity=Array.from({length:12},(_,i)=>({...p,revision:String(i),name:`Transfer ${i}`}));
    const props={state,activity,open:true,onOpen:vi.fn(),onSettings:vi.fn(),onRefresh:vi.fn()};
    const view=render(<SyncStatus {...props}/>);
    expect(document.querySelectorAll('.sync-history li')).toHaveLength(5);
    expect(screen.getByRole('button',{name:'Previous'})).toBeDisabled();
    await user.click(screen.getByRole('button',{name:'Next'}));
    expect(screen.getByText('Transfer 5',{exact:true})).toBeVisible();
    act(()=>view.rerender(<SyncStatus {...props} state={{...state,syncInfo:{...idleSync(),phase:'synced',lastContact:Date.now()}}}/>));
    expect(screen.getByText('Page 2 of 3')).toBeVisible();
    expect(screen.getByRole('button',{name:'Next'})).toHaveFocus();
    await user.click(screen.getByRole('button',{name:'Next'}));
    expect(document.querySelectorAll('.sync-history li')).toHaveLength(2);
    expect(screen.getByRole('button',{name:'Next'})).toBeDisabled();
    view.rerender(<SyncStatus {...props} activity={activity.slice(0,6)}/>);
    expect(screen.getByText('Page 2 of 2')).toBeVisible();
    view.rerender(<SyncStatus {...props} open={false}/>);view.rerender(<SyncStatus {...props}/>);
    expect(screen.getByText('Page 1 of 3')).toBeVisible();
  });
});
