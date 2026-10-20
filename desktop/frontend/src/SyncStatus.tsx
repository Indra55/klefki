import {useEffect,useRef,useState} from 'react';
import * as Dialog from '@radix-ui/react-dialog';
import {X} from 'lucide-react';
import {api, type State} from './api';
import {idleSync, syncLabel, phaseLabel, syncIndicator, syncLegend, activityPage, transferDetail, deviceName, presence, receipt, devicePendingDetail, waitingDevices, bytes, TransferRate, type SyncInfo, type SyncActivity, type DeviceStatus} from '../../../ui/sync-status';

export function syncInfo(state: State): SyncInfo {
  return state.syncInfo || {...idleSync(),phase:state.sync==='Synced'?'synced':state.sync==='Syncing'?'checking':state.sync==='Pending'?'pending':'local',running:state.sync==='Syncing'};
}
export function SyncPauseControl({state,onRefresh}:{state:State;onRefresh:()=>Promise<void>}) {
  const [saving,setSaving]=useState(false),[error,setError]=useState('');
  return <><button className="quiet" disabled={saving} onClick={()=>{setSaving(true);setError('');void api().SetSyncPaused(!syncInfo(state).paused).then(onRefresh).catch(()=>setError('Could not save the sync preference. Try again.')).finally(()=>setSaving(false));}}>{syncInfo(state).paused?'Resume sync':'Pause sync'}</button>{error&&<p role="alert">{error}</p>}</>;
}
function useClock() {
  const [now,setNow]=useState(Date.now());
  useEffect(()=>{const timer=window.setInterval(()=>setNow(Date.now()),1000);return()=>clearInterval(timer)},[]);
  return now;
}
export function SyncStrip({state,onDetails}:{state:State;onDetails?:()=>void}) {
  const info=syncInfo(state),now=useClock(),rate=useRef(new TransferRate()),p=info.progress;
  const speed=p?rate.current.update(p):'';
  if (!info.running) return info.paused?<section className="sync-strip" aria-label="Sync paused"><div><strong>{syncLabel(info,state.syncDevice||'',state.deviceAliases||{})}</strong><p>Local changes stay queued until you resume.</p></div>{onDetails&&<button className="quiet" onClick={onDetails}>Details</button>}</section>:null;
  return <section className="sync-strip" aria-label="Sync progress"><div><strong>{info.paused?syncLabel(info,state.syncDevice||'',state.deviceAliases||{}):phaseLabel(info.phase)}</strong><p>{p?transferDetail(p,state.deviceAliases||{},info.peers?.devices||[],state.syncDevice||'',now):'Connecting to your server…'}</p>{speed&&<p className="sync-speed">{speed}</p>}{(info.transfers?.length||0)>1&&<ul className="sync-active" aria-label="Active sync workers">{info.transfers!.map(t=><li key={t.direction+':'+t.revision}>{phaseLabel(t.phase)} · {t.name}</li>)}</ul>}</div>{onDetails&&<button className="quiet" onClick={onDetails}>Details</button>}<progress aria-label="Document sync progress" max={p?.totalBytes||1} value={p?.totalBytes&&p.phase!=='verifying'?p.doneBytes:undefined}/></section>;
}
export function SyncDevices({state,now:time,onRefresh,onForget}:{state:State;now?:number;onRefresh:()=>Promise<void>;onForget?:(id:string)=>void}) {
  const clock=useClock(),now=time||clock;
  const info=syncInfo(state),aliases=state.deviceAliases||{},self=state.syncDevice||'';
  const peers=info.peers?.devices||state.syncDevices?.map(device=>({device,clientKind:'',phase:'unknown',lastSeen:0,sequence:0,outstanding:0,doneBytes:0,totalBytes:0}))||[];
  return <div className="sync-devices">{peers.map(d=><DeviceRow key={d.device} device={d} name={deviceName(d.device,aliases,peers,self)} alias={aliases[d.device]||''} self={d.device===self} now={now} checked={info.lastDeviceCheck} fresh={info.statusAvailable&&now-info.lastDeviceCheck<45000} onRefresh={onRefresh} onForget={onForget}/>)}</div>;
}
function DeviceRow({device:d,name,alias,self,now,checked,fresh,onRefresh,onForget}:{device:DeviceStatus;name:string;alias:string;self:boolean;now:number;checked:number;fresh:boolean;onRefresh:()=>Promise<void>;onForget?:(id:string)=>void}) {
  const [value,setValue]=useState(alias),[error,setError]=useState(''),[saving,setSaving]=useState(false);
  useEffect(()=>setValue(alias),[alias]);
  return <div className="sync-device"><div className="sync-device-heading"><strong>{name}{self&&name!=='This device'?' · This device':''}</strong><small>{presence(d,now,checked)}</small></div><p>{receipt(d,fresh)}</p>{d.deviceState?.pending&&<p>{devicePendingDetail(d,fresh)}</p>}{d.deviceState&&<small>Pending-state report {new Date(d.deviceState.reportedAt).toLocaleString()}</small>}{d.lastSeen>0&&<small>Last contact {new Date(d.lastSeen).toLocaleString()}</small>}{fresh&&d.phase!=='unknown'&&d.totalBytes>0&&<p>{bytes(d.doneBytes)} of {bytes(d.totalBytes)} · {phaseLabel(d.phase)}</p>}{api().SetDeviceAlias&&<form onSubmit={e=>{e.preventDefault();setSaving(true);setError('');void api().SetDeviceAlias!(d.device,value).then(onRefresh).catch(()=>setError('Could not save this nickname.')).finally(()=>setSaving(false))}}><input aria-label={`Nickname for device ${d.device.slice(0,8)}`} placeholder={name} value={value} maxLength={64} disabled={saving} onChange={e=>setValue(e.target.value)}/><button className="quiet" disabled={saving||value.trim()===alias}>Save</button></form>}{onForget&&!self&&<button className="quiet" onClick={()=>onForget(d.device)}>Forget device</button>}{error&&<p role="alert">{error}</p>}</div>;
}
export function SyncStatus({state,activity,open,onOpen,onSettings,onRefresh}:{state:State;activity:SyncActivity[];open:boolean;onOpen:(value:boolean)=>void;onSettings:()=>void;onRefresh:()=>Promise<void>}) {
  const info=syncInfo(state),now=useClock(),aliases=state.deviceAliases||{},self=state.syncDevice||'',indicator=syncIndicator(info,self,aliases,state.pending,now),waiting=waitingDevices(info,self,now);
  useEffect(()=>{void api().SetSyncPanel?.(open);return()=>{void api().SetSyncPanel?.(false)}},[open]);
  const [error,setError]=useState(''),[refreshing,setRefreshing]=useState(false),[pageIndex,setPage]=useState(0);
  const page=activityPage(activity,pageIndex);
  useEffect(()=>{if(open)setPage(0)},[open]);
  useEffect(()=>setPage(page.page),[page.page]);
  return <Dialog.Root open={open} onOpenChange={onOpen}>
    <Dialog.Trigger className="sync-trigger" aria-label="Sync status" aria-description={indicator.label} title={indicator.label} data-state={indicator.state}>
      <span className="sync-dot" aria-hidden="true"/><span className="sr-only" aria-live="polite" aria-atomic="true">{indicator.label}</span>
    </Dialog.Trigger>
    <Dialog.Portal><Dialog.Overlay className="sync-backdrop"/><Dialog.Content className="sync-panel">
      <header><Dialog.Title>Sync activity</Dialog.Title><Dialog.Close className="icon" aria-label="Close sync activity"><X size={17}/></Dialog.Close></header>
      <Dialog.Description className={indicator.state==='error'?'sync-error':undefined}>{indicator.label}</Dialog.Description>
      <p className="sync-address">{state.config.server||'Connect a server in Settings to sync.'}</p>
      <p>{state.pending?`${state.pending} changes waiting to upload.`:'No local changes waiting to upload.'}</p>
      {!info.running&&waiting.length>0&&info.waitingFor!=='source'&&<p>Saved on server; waiting for {waiting.map(d=>deviceName(d.device,aliases,info.peers?.devices,self)).join(', ')}.</p>}
      {info.lastContact>0&&<p>Last successful server contact {new Date(info.lastContact).toLocaleString()}</p>}
      {state.lastSync&&<p>Last sync completed {new Date(state.lastSync).toLocaleString()}</p>}
      <SyncStrip state={state}/>
      <div className="sync-controls">
        {state.config.server&&!state.recoveryOnly&&<button className="quiet" disabled={info.running||refreshing||info.paused} onClick={()=>{
          setRefreshing(true);setError('');void api().RefreshSync().catch(()=>setError('Could not start sync.')).finally(()=>setRefreshing(false));
        }}>Refresh sync</button>}
        <SyncPauseControl state={state} onRefresh={onRefresh}/><button className="quiet" onClick={()=>{onOpen(false);onSettings()}}>Connection settings</button>
      </div>
      {error&&<p className="sync-error" role="alert">{error}</p>}
      <div className="sync-legend" aria-label="Sync color legend">{syncLegend.map(item=><span key={item.state} data-state={item.state}><span className="sync-dot" aria-hidden="true"/>{item.label}</span>)}</div>
      <h3>Linked devices</h3>{!info.statusAvailable&&<p>Device status unavailable. Your documents can still sync.</p>}
      <SyncDevices state={state} now={now} onRefresh={onRefresh}/><p>Nicknames are saved only on this device.</p>
      <h3>Recent activity</h3>{!activity.length&&<p>No transfers in this unlocked session yet.</p>}
      <ul className="sync-history">{page.items.map(p=><li key={`${p.direction}:${p.revision}`}><strong>{p.name||'Library changes'}</strong><p>{phaseLabel(p.phase)} · {p.direction==='download'?`from ${deviceName(p.source||'',aliases,info.peers?.devices,self)} via server`:'to server'}</p>{p.retryAt&&<p>{transferDetail(p,aliases,info.peers?.devices||[],self,now)}</p>}{p.publisher&&p.publisher!==p.source&&<p>Last published by {deviceName(p.publisher,aliases,info.peers?.devices,self)}</p>}</li>)}</ul>
      {page.pages>1&&<nav className="sync-pagination" aria-label="Recent activity pages"><button className="quiet" disabled={page.page===0} onClick={()=>setPage(page.page-1)}>Previous</button><span aria-live="polite">Page {page.page+1} of {page.pages}</span><button className="quiet" disabled={page.page===page.pages-1} onClick={()=>setPage(page.page+1)}>Next</button></nav>}
    </Dialog.Content></Dialog.Portal>
  </Dialog.Root>;
}
