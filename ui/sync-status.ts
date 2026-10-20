export type SyncProgress = {
  phase: string; revision?: string; name?: string; direction?: 'upload' | 'download'; source?: string; publisher?: string;
  doneBytes: number; totalBytes: number; reusedBytes: number; chunk: number; chunks: number;
  attempt?: number; retryAt?: number; updatedAt: number;
  lane?: 'library' | 'receipts'; status?: number;
};
export type DeviceState = {version:number;pending:boolean;reportedAt:number};
export type DeviceStatus = {
  deviceState?:DeviceState;
  device: string; clientKind: string; phase: string; revision?: string; direction?: string;
  doneBytes: number; totalBytes: number; lastSeen: number; sequence: number; outstanding: number;
};
export type ServerStatus = { deviceStateSupported?:boolean; epoch: string; latestSequence: number; devices: DeviceStatus[] };
export type Provenance = { sequence: number; revision: string; origin: string; publisher: string };
export type SyncInfo = {
  transfers?:SyncProgress[];
  paused?: boolean;
  phase: string; running: boolean; progress?: SyncProgress; lastContact: number;
  lastDeviceCheck: number; statusAvailable: boolean; peers?: ServerStatus;
  waitingFor?: 'source' | 'receipts';
};
export type SyncActivity = SyncProgress;
export const idleSync = (): SyncInfo => ({phase: 'local', running: false, lastContact: 0, lastDeviceCheck: 0, statusAvailable: false});
const labels: Record<string,string> = {
  local: 'Local only', locked: 'Locked', connecting: 'Checking connection', checking: 'Checking for changes',
  pending: 'Changes waiting', uploading: 'Uploading', downloading: 'Downloading', verifying: 'Verifying',
  committing: 'Saving changes', acknowledging: 'Confirming receipt', complete: 'Saved', synced: 'Up to date with server',
  waiting: 'Waiting for devices', retrying: 'Retrying', unreachable: 'Server unreachable', offline: 'Network unavailable',
  timeout: 'Connection timed out', auth: 'Sign-in required', full: 'Relay storage full', error: 'Sync error',
};
export function phaseLabel(phase: string) { return labels[phase] || 'Checking sync'; }
export function bytes(n: number) { return n < 1024 ? `${n} B` : n < 1048576 ? `${(n/1024).toFixed(0)} KB` : `${(n/1048576).toFixed(1)} MB`; }
export function deviceName(id: string, aliases: Record<string,string>, devices: DeviceStatus[] = [], self = '') {
  if (!id) return 'Source unknown';
  if (aliases[id]) return aliases[id];
  if (id === self) return 'This device';
  const kind = devices.find(d => d.device === id)?.clientKind;
  const label = kind === 'desktop' ? 'Desktop' : kind === 'pwa' ? 'PWA' : kind === 'web' ? 'Browser' : kind === 'cli' ? 'CLI' : 'Device';
  return `${label} ${id.slice(0,8)}`;
}
export function devicePendingDetail(d: DeviceStatus, fresh: boolean) {
  if (!d.deviceState?.pending) return '';
  return `${fresh ? 'Last reported' : 'Last known report:'} changes waiting to upload. Reopen this device to finish syncing.`;
}
export function sourceDevices(info: SyncInfo, self: string) {
  return info.peers?.devices.filter(d => d.device !== self && d.deviceState?.pending) || [];
}
export function receipt(d: DeviceStatus, fresh: boolean) {
  if (!fresh || !d.lastSeen) return 'Status unknown';
  return d.outstanding ? `${d.outstanding} ${d.outstanding === 1 ? 'change' : 'changes'} waiting` : d.deviceState?.pending ? 'Received latest server changes' : 'Received latest changes';
}
export function presence(d: DeviceStatus, now = Date.now(), checked = now) {
  if (now-checked >= 45000 || !d.lastSeen || now-d.lastSeen >= 45000 || d.phase === 'unknown' || d.phase === 'locked') return 'Not recently seen';
  return d.phase === 'idle' ? 'Recently connected' : phaseLabel(d.phase);
}
export function waitingDevices(info: SyncInfo, self: string, now = Date.now()) {
  if (!info.statusAvailable || now-info.lastDeviceCheck >= 45000) return [];
  return info.peers?.devices.filter(d => d.device !== self && d.outstanding > 0) || [];
}
export function syncLabel(info: SyncInfo, self: string, aliases: Record<string,string>, now = Date.now()) {
  if (info.paused) return info.running ? 'Pausing after current sync' : 'Sync paused on this device';
  if (info.phase === 'waiting' && info.waitingFor === 'source') return 'Waiting for another device to supply changes';
  const sources = sourceDevices(info,self);
  if (!info.running && ['synced','waiting'].includes(info.phase) && sources.length && info.statusAvailable && now-info.lastDeviceCheck < 45000)
    return `${deviceName(sources[0].device,aliases,info.peers?.devices,self)} reported changes waiting to upload`;
  const waiting = waitingDevices(info,self,now);
  if (!info.running && info.phase === 'synced' && waiting.length) return `Waiting for ${waiting.length === 1 ? deviceName(waiting[0].device,aliases,info.peers?.devices,self) : `${waiting.length} devices`}`;
  return phaseLabel(info.phase);
}
export type SyncIndicatorState = 'synced' | 'pending' | 'waiting' | 'running' | 'error' | 'neutral';
export const syncLegend: {state:SyncIndicatorState;label:string}[] = [
  {state:'synced',label:'Synced'}, {state:'pending',label:'Local changes waiting'},
  {state:'waiting',label:'Device changes waiting'}, {state:'running',label:'Syncing'},
  {state:'error',label:'Server or sync error'}, {state:'neutral',label:'Offline / local / unknown'},
];
export function syncIndicator(info: SyncInfo, self: string, aliases: Record<string,string>, pending = 0, now = Date.now()): {state:SyncIndicatorState;label:string} {
  const label=syncLabel(info,self,aliases,now);
  if (info.paused) return {state:info.running?'running':'neutral',label};
  if (['error','auth','unreachable','timeout','full'].includes(info.phase)) return {state:'error',label};
  if (['offline','local','locked'].includes(info.phase)) return {state:'neutral',label};
  if (info.running) return {state:'running',label};
  if (pending > 0 || info.phase === 'pending') return {state:'pending',label:'Local changes waiting to upload'};
  if (info.phase === 'waiting') return {state:'waiting',label};
  const fresh=info.statusAvailable && now-info.lastDeviceCheck < 45000;
  if (fresh && info.peers?.devices.some(d=>d.device===self && d.outstanding>0)) return {state:'waiting',label:'Changes waiting to download'};
  if (fresh && sourceDevices(info,self).length) return {state:'waiting',label};
  if (!fresh && sourceDevices(info,self).length) return {state:'neutral',label:'Last known report: another device had changes waiting to upload'};
  if (info.phase === 'synced') return {state:waitingDevices(info,self,now).length?'waiting':'synced',label};
  return {state:'neutral',label:'Sync status unknown'};
}
export const activityPageSize = 5;
export function activityPage(activity: SyncActivity[], requested: number) {
  const pages=Math.max(1,Math.ceil(activity.length/activityPageSize)),page=Math.max(0,Math.min(requested,pages-1));
  return {page,pages,items:activity.slice(page*activityPageSize,(page+1)*activityPageSize)};
}
export function transferDetail(p: SyncProgress, aliases: Record<string,string>, devices: DeviceStatus[], self: string, now = Date.now()) {
  const detail = [p.name || 'Library changes'];
  if (p.direction === 'download') detail.push(`from ${deviceName(p.source || '',aliases,devices,self)} via server`);
  else if (p.direction === 'upload') detail.push('to server');
  if (p.totalBytes) detail.push(`${bytes(p.doneBytes)} of ${bytes(p.totalBytes)}`);
  if (p.chunks) detail.push(`chunk ${p.chunk || 1} of ${p.chunks}`);
  if (p.reusedBytes) detail.push(`${bytes(p.reusedBytes)} already saved`);
  if(p.status)detail.push(p.status===429?'Server rate limit':`Server returned ${p.status}`);
  if(p.phase==='retrying')detail.push(`attempt ${p.attempt || 2} in ${Math.max(0,Math.ceil(((p.retryAt || now)-now)/1000))}s`);
  else if (p.retryAt) detail.push(p.retryAt>=Number.MAX_SAFE_INTEGER?'Needs repair; refresh sync after correcting the file':`retry in ${Math.max(0,Math.ceil((p.retryAt-now)/1000))}s`);
  else if (now-p.updatedAt >= 10000 && p.phase !== 'complete') detail.push('Taking longer than usual');
  return detail.join(' · ');
}
export function addActivity(list: SyncActivity[], p: SyncProgress) {
  if (!p.revision&&!p.lane) return list;
  const index = list.findIndex(x => x.revision === p.revision && x.direction === p.direction&&x.lane===p.lane);
  if (index < 0) return [p,...list].slice(0,50);
  return list.map((x,i) => i === index ? p : x);
}
export function validateStatus(value: ServerStatus) {
  const id = (s: unknown) => typeof s === 'string' && /^[0-9a-f]{32}$/.test(s) && !/^0+$/.test(s);
  if (!value || !id(value.epoch) || !Number.isSafeInteger(value.latestSequence) || value.latestSequence < 0 || !Array.isArray(value.devices) || value.devices.length > 64) throw Error('Invalid device status');
  if (value.deviceStateSupported !== undefined && typeof value.deviceStateSupported !== 'boolean') throw Error('Invalid device status');
  const seen = new Set<string>();
  for (const d of value.devices) {
    if (!id(d.device) || seen.has(d.device) || !Number.isSafeInteger(d.sequence) || d.sequence < 0 || d.sequence > value.latestSequence || !Number.isSafeInteger(d.outstanding) || d.outstanding < 0 || !Number.isSafeInteger(d.lastSeen) || d.lastSeen < 0 || !Number.isSafeInteger(d.doneBytes) || !Number.isSafeInteger(d.totalBytes) || d.doneBytes < 0 || d.doneBytes > d.totalBytes || typeof d.phase !== 'string' || typeof d.clientKind !== 'string') throw Error('Invalid device status');
    if (d.deviceState && (!Number.isSafeInteger(d.deviceState.version) || d.deviceState.version < 1 || typeof d.deviceState.pending !== 'boolean' || !Number.isSafeInteger(d.deviceState.reportedAt) || d.deviceState.reportedAt < 0)) throw Error('Invalid device status');
    seen.add(d.device);
  }
  return value;
}

export class TransferRate {
  private revision = ''; private direction = ''; private started = 0; private baseline = 0; private samples = new Map<number,number>();
  update(p: SyncProgress) {
    if (p.revision !== this.revision || p.direction !== this.direction || p.phase === 'retrying') {
      this.revision = p.revision || ''; this.direction = p.direction || ''; this.started = p.updatedAt; this.baseline = Math.max(0,p.doneBytes-p.reusedBytes);this.samples.clear();
    }
    const sent = p.doneBytes-p.reusedBytes-this.baseline;
    if (sent > 0 && p.chunk && p.doneBytes >= Math.min(p.totalBytes,p.chunk*4194304)) this.samples.set(p.chunk,sent);
    const elapsed = (p.updatedAt-this.started)/1000;
    if (p.phase === 'retrying' || this.samples.size < 2 || elapsed < 5 || sent <= 0) return '';
    const speed = sent/elapsed, remaining = Math.max(0,p.totalBytes-p.doneBytes);
    return `${bytes(speed)}/s${remaining ? ` · about ${Math.ceil(remaining/speed)}s remaining` : ''}`;
  }
}
