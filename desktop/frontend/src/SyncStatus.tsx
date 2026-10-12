import {useEffect,useRef,useState} from 'react';
import * as Dialog from '@radix-ui/react-dialog';
import {X} from 'lucide-react';
import {api, type State} from './api';
import {idleSync, phaseLabel, syncLabel, transferDetail, deviceName, presence, receipt, waitingDevices, bytes, TransferRate, type SyncInfo, type SyncActivity, type DeviceStatus} from '../../../ui/sync-status';

export function syncInfo(state: State): SyncInfo {
  return state.syncInfo || {...idleSync(),phase:state.sync==='Synced'?'synced':state.sync==='Syncing'?'checking':state.sync==='Pending'?'pending':'local',running:state.sync==='Syncing'};
}
function useClock() {
  const [now,setNow]=useState(Date.now());
  useEffect(()=>{const timer=window.setInterval(()=>setNow(Date.now()),1000);return()=>clearInterval(timer)},[]);
  return now;
}
export function SyncStrip({state,onDetails}:{state:State;onDetails?:()=>void}) {
  const info=syncInfo(state),now=useClock(),rate=useRef(new TransferRate()),p=info.progress;
  const speed=p?rate.current.update(p):'';
  if (!info.running) return null;
  return <section className="sync-strip" aria-label="Sync progress"><div><strong>{phaseLabel(info.phase)}</strong><p>{p?transferDetail(p,state.deviceAliases||{},info.peers?.devices||[],state.syncDevice||'',now):'Connecting to your server…'}</p>{speed&&<p className="sync-speed">{speed}</p>}</div>{onDetails&&<button className="quiet" onClick={onDetails}>Details</button>}<progress aria-label="Document sync progress" max={p?.totalBytes||1} value={p?.totalBytes&&p.phase!=='verifying'?p.doneBytes:undefined}/></section>;
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
  return <div className="sync-device"><div className="sync-device-heading"><strong>{name}{self&&name!=='This device'?' · This device':''}</strong><small>{presence(d,now,checked)}</small></div><p>{receipt(d,fresh)}</p>{d.lastSeen>0&&<small>Last contact {new Date(d.lastSeen).toLocaleString()}</small>}{fresh&&d.phase!=='unknown'&&d.totalBytes>0&&<p>{bytes(d.doneBytes)} of {bytes(d.totalBytes)} · {phaseLabel(d.phase)}</p>}{api().SetDeviceAlias&&<form onSubmit={e=>{e.preventDefault();setSaving(true);setError('');void api().SetDeviceAlias!(d.device,value).then(onRefresh).catch(()=>setError('Could not save this nickname.')).finally(()=>setSaving(false))}}><input aria-label={`Nickname for device ${d.device.slice(0,8)}`} placeholder={name} value={value} maxLength={64} disabled={saving} onChange={e=>setValue(e.target.value)}/><button className="quiet" disabled={saving||value.trim()===alias}>Save</button></form>}{onForget&&!self&&<button className="quiet" onClick={()=>onForget(d.device)}>Forget device</button>}{error&&<p role="alert">{error}</p>}</div>;
}
export function SyncStatus({state,activity,open,onOpen,onSettings,onRefresh}:{state:State;activity:SyncActivity[];open:boolean;onOpen:(value:boolean)=>void;onSettings:()=>void;onRefresh:()=>Promise<void>}) {
  const info=syncInfo(state),now=useClock(),aliases=state.deviceAliases||{},self=state.syncDevice||'',label=syncLabel(info,self,aliases),waiting=waitingDevices(info,self);
  useEffect(()=>{void api().SetSyncPanel?.(open);return()=>{void api().SetSyncPanel?.(false)}},[open]);
  const [error,setError]=useState('');
  return <Dialog.Root open={open} onOpenChange={onOpen}><Dialog.Trigger className="sync-trigger" aria-label="Sync status" title={label} data-state={info.running?'running':['error','auth','offline','unreachable','timeout','full'].includes(info.phase)?'error':'idle'}><span className="sync-dot"/><span aria-live="polite" aria-atomic="true">{label}</span></Dialog.Trigger><Dialog.Portal><Dialog.Overlay className="sync-backdrop"/><Dialog.Content className="sync-panel"><header><Dialog.Title>Sync activity</Dialog.Title><Dialog.Close className="icon" aria-label="Close sync activity"><X size={17}/></Dialog.Close></header><Dialog.Description>{label}</Dialog.Description><p className="sync-address">{state.config.server||'Connect a server in Settings to sync.'}</p><p>{state.pending?`${state.pending} changes waiting to upload.`:'No local changes waiting to upload.'}</p>{!info.running&&waiting.length>0&&<p>Saved on server; waiting for {waiting.map(d=>deviceName(d.device,aliases,info.peers?.devices,self)).join(', ')}.</p>}{info.lastContact>0&&<p>Last successful server contact {new Date(info.lastContact).toLocaleString()}</p>}{state.lastSync&&<p>Last sync completed {new Date(state.lastSync).toLocaleString()}</p>}<SyncStrip state={state}/><div className="sync-controls">{state.config.server&&!state.recoveryOnly&&<button className="quiet" disabled={info.running} onClick={()=>{onOpen(false);setError('');void api().SyncNow().catch(()=>setError('Could not start sync.'))}}>{info.phase==='auth'?'Reconnect':'Sync now'}</button>}<button className="quiet" onClick={()=>{onOpen(false);onSettings()}}>Connection settings</button></div>{error&&<p className="sync-error" role="alert">{error}</p>}<h3>Linked devices</h3>{!info.statusAvailable&&<p>Device status unavailable. Your documents can still sync.</p>}<SyncDevices state={state} now={now} onRefresh={onRefresh}/><p>Nicknames are saved only on this device.</p><h3>Recent activity</h3>{!activity.length&&<p>No transfers in this unlocked session yet.</p>}<ul className="sync-history">{activity.map(p=><li key={`${p.direction}:${p.revision}`}><strong>{p.name||'Library changes'}</strong><p>{phaseLabel(p.phase)} · {p.direction==='download'?`from ${deviceName(p.source||'',aliases,info.peers?.devices,self)} via server`:'to server'}</p>{p.publisher&&p.publisher!==p.source&&<p>Last published by {deviceName(p.publisher,aliases,info.peers?.devices,self)}</p>}</li>)}</ul></Dialog.Content></Dialog.Portal></Dialog.Root>;
}
