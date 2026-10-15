import { cleanup, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { Settings } from './Settings';
import type { State } from './api';

const state: State = { config: {accountPath:'/vault/account.v1',storePath:'/vault/local.db',server:'https://relay.example',preferences:{theme:'dark',idleMinutes:5}}, unlocked:true,generation:1,accountID:'account',recoveryOnly:false,sync:'Synced',pending:0,lastSync:'',syncInfo:{phase:'synced',running:false,lastContact:0,lastDeviceCheck:0,statusAvailable:false} };
function show(value=state) {
  const onRefresh=vi.fn(async()=>{});
  render(<Settings state={value} section="sync" onSectionChange={()=>{}} scan="" onScanStart={()=>{}} onClose={()=>{}} onRefresh={onRefresh} onRebuildIndex={()=>{}} indexing={false} operationError=""/>);
  return onRefresh;
}
describe('fresh download confirmation',()=>{
  afterEach(cleanup);
  beforeEach(()=>{window.go={main:{App:{FreshDownload:vi.fn(async()=>{})} as any}};});
  it('requires explicit confirmation and keeps cancellation local',async()=>{
    const refresh=show();
    await userEvent.click(screen.getByRole('button',{name:'Download a fresh copy'}));
    expect(screen.getByText(/Keep the current store at \/vault\/local.db/)).toBeVisible();
    expect(window.go!.main.App.FreshDownload).not.toHaveBeenCalled();
    await userEvent.click(screen.getByRole('button',{name:'Cancel'}));
    expect(window.go!.main.App.FreshDownload).not.toHaveBeenCalled();
    await userEvent.click(screen.getByRole('button',{name:'Download a fresh copy'}));
    await userEvent.click(screen.getByRole('button',{name:'Verify and use fresh copy'}));
    expect(window.go!.main.App.FreshDownload).toHaveBeenCalledOnce();
    expect(refresh).toHaveBeenCalledOnce();
  });
  it('explains a missing relay source without reporting success',async()=>{
    window.go!.main.App.FreshDownload=vi.fn(async()=>{throw new Error('RELAY_WAITING');});
    show();
    await userEvent.click(screen.getByRole('button',{name:'Download a fresh copy'}));
    await userEvent.click(screen.getByRole('button',{name:'Verify and use fresh copy'}));
    expect(await screen.findByRole('alert')).toHaveTextContent('Reconnect an existing linked device');
    expect(screen.getByRole('button',{name:'Download a fresh copy'})).toBeEnabled();
  });
  it('disables replacement when this device has local changes',()=>{
    show({...state,pending:2});
    expect(screen.getByRole('button',{name:'Download a fresh copy'})).toBeDisabled();
  });
});
