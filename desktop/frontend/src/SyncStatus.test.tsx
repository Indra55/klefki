import {beforeEach,afterEach,describe,it,expect,vi} from 'vitest';
import {render,screen,waitFor,act,cleanup} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import App from './App';
import {idleSync,receipt,presence,TransferRate,type SyncProgress} from '../../../ui/sync-status';
import type {State,KlefkiEvent} from './api';

const self='11111111111111111111111111111111',phone='22222222222222222222222222222222';
let state:State,emit:(event:KlefkiEvent)=>void;
const p:SyncProgress={phase:'uploading',revision:'33333333333333333333333333333333',name:'private-report.pdf',direction:'upload',doneBytes:4194304,totalBytes:8388608,reusedBytes:0,chunk:1,chunks:2,updatedAt:Date.now()};
beforeEach(()=>{
  state={config:{accountPath:'account',storePath:'store',server:'https://sync.test',preferences:{theme:'dark',idleMinutes:5}},unlocked:true,generation:1,accountID:'account',recoveryOnly:false,sync:'Synced',pending:1,lastSync:'',syncDevice:self,syncDevices:[self,phone],deviceAliases:{[phone]:'My phone'},syncInfo:{...idleSync(),phase:'synced'}};
  window.matchMedia=vi.fn().mockReturnValue({matches:false,addEventListener:vi.fn(),removeEventListener:vi.fn()});
  window.runtime={EventsOn:(name,cb)=>{if(name==='klefki:event')emit=cb;return()=>{}}};
  window.go={main:{App:{State:vi.fn(async()=>state),List:vi.fn(()=>new Promise(()=>{})),Touch:vi.fn(),SyncNow:vi.fn(async()=>{}),SetSyncPanel:vi.fn(async()=>{}),SetDeviceAlias:vi.fn(async()=>{}),ClosePreview:vi.fn()} as any}};
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
});
