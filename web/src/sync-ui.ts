import {node,button,icon} from './ui.ts';
import {idleSync,syncLabel,phaseLabel,deviceName,transferDetail,receipt,presence,waitingDevices,bytes,TransferRate,type SyncInfo,type SyncActivity} from '../../ui/sync-status.ts';

type Data = {info:SyncInfo;pending:number;server:string;aliases:Record<string,string>;self:string;activity:SyncActivity[];lastSync:number;devices:string[]};
type Actions = {sync:()=>void;settings:()=>void;panel:(open:boolean)=>void;alias:(id:string,name:string)=>Promise<void>};
export class SyncView {
  private data:Data={info:idleSync(),pending:0,server:'',aliases:{},self:'',activity:[],lastSync:0,devices:[]};
  private trigger?:HTMLButtonElement;
  private label?:HTMLSpanElement;
  private strip?:HTMLElement;
  private dialog?:HTMLDialogElement;
  private summary?:HTMLElement;
  private deviceList?:HTMLElement;
  private history?:HTMLElement;
  private transfer?:HTMLElement;
  private syncButton?:HTMLButtonElement;
  private rate=new TransferRate();
  constructor(private actions:Actions) {}
  get open() { return !!this.dialog?.open; }
  mount(actions:HTMLElement,root:HTMLElement) {
    this.trigger=button('',()=>this.show(),'sync-trigger');this.trigger.setAttribute('aria-label','Sync status');this.trigger.setAttribute('aria-haspopup','dialog');
    const dot=node('span',undefined,'sync-dot');this.label=node('span');this.label.setAttribute('aria-live','polite');this.label.setAttribute('aria-atomic','true');this.trigger.append(dot,this.label);actions.append(this.trigger);
    this.strip=node('section',undefined,'sync-strip');this.strip.setAttribute('aria-label','Sync progress');root.append(this.strip);
    this.render();
  }
  update(data:Data) { this.data=data;this.render(); }
  clear() { this.dialog?.close();this.dialog?.remove();this.dialog=undefined;this.trigger=undefined;this.strip=undefined;this.rate=new TransferRate();this.data={info:idleSync(),pending:0,server:'',aliases:{},self:'',activity:[],lastSync:0,devices:[]}; }
  private show() {
    if (!this.dialog) {
      const dialog=node('dialog',undefined,'sync-panel');dialog.setAttribute('aria-label','Sync activity');
      const header=node('header');header.append(node('h2','Sync activity'),button('Close',()=>dialog.close(),'quiet'));
      this.summary=node('div');this.transfer=node('section',undefined,'sync-strip');
      const controls=node('div',undefined,'sync-controls');this.syncButton=button('Sync now',()=>{dialog.close();this.actions.sync()},'quiet');controls.append(this.syncButton,button('Connection settings',()=>{dialog.close();this.actions.settings()},'quiet'));
      this.deviceList=node('div',undefined,'sync-devices');this.history=node('ul',undefined,'sync-history');
      dialog.append(header,this.summary,this.transfer,controls,node('h3','Linked devices'),this.deviceList,node('p','Nicknames are saved only on this device.'),node('h3','Recent activity'),this.history);
      dialog.onclose=()=>{this.actions.panel(false);this.trigger?.setAttribute('aria-expanded','false');this.trigger?.focus()};
      dialog.onclick=e=>{if(e.target===dialog){const rect=dialog.getBoundingClientRect();if(e.clientX<rect.left||e.clientX>rect.right||e.clientY<rect.top||e.clientY>rect.bottom)dialog.close()}};
      document.body.append(dialog);this.dialog=dialog;
    }
    this.dialog.showModal();this.trigger?.setAttribute('aria-expanded','true');this.actions.panel(true);this.render();
  }
  private progress(container:HTMLElement,details:boolean) {
    const {info,aliases,self}=this.data,p=info.progress;
    container.hidden=!info.running;
    if (!info.running) return;
    let text=container.querySelector<HTMLDivElement>('.sync-copy');
    if(!text){text=node('div',undefined,'sync-copy');text.append(node('strong'),node('p',undefined,'sync-detail'),node('p',undefined,'sync-speed'));container.append(text)}
    text.querySelector('strong')!.textContent=phaseLabel(info.phase);
    text.querySelector('.sync-detail')!.textContent=p?transferDetail(p,aliases,info.peers?.devices||[],self):'Connecting to your server…';
    const speed=p?this.rate.update(p):'',speedNode=text.querySelector<HTMLElement>('.sync-speed')!;speedNode.textContent=speed;speedNode.hidden=!speed;
    if(details&&!container.querySelector('button'))container.append(button('Details',()=>this.show(),'quiet'));
    let progress=container.querySelector('progress');if(!progress){progress=node('progress');progress.setAttribute('aria-label','Document sync progress');container.append(progress)}
    progress.max=p?.totalBytes||1;if(p?.totalBytes&&p.phase!=='verifying')progress.value=p.doneBytes;else progress.removeAttribute('value');
  }
  private render() {
    const {info,self,aliases,pending,server,lastSync,activity}=this.data,now=Date.now(),peers=info.peers?.devices||[];
    const label=syncLabel(info,self,aliases);
    if(this.label)this.label.textContent=label;
    if(this.trigger){this.trigger.title=label;this.trigger.dataset.state=info.running?'running':['error','auth','offline','unreachable','timeout','full'].includes(info.phase)?'error':'idle';}
    if(this.strip)this.progress(this.strip,true);
    if(!this.open||!this.summary||!this.deviceList||!this.history||!this.transfer)return;
    this.summary.replaceChildren(node('p',label),node('p',server,'sync-address'),node('p',pending?`${pending} changes waiting to upload.`:'No local changes waiting to upload.'));
    const waiting=waitingDevices(info,self);if(!info.running&&waiting.length)this.summary.append(node('p',`Saved on server; waiting for ${waiting.map(d=>deviceName(d.device,aliases,peers,self)).join(', ')}.`));
    if(info.lastContact)this.summary.append(node('p','Last successful server contact '+new Date(info.lastContact).toLocaleString()));
    if(lastSync)this.summary.append(node('p','Last sync completed '+new Date(lastSync).toLocaleString()));
    if(!info.statusAvailable)this.summary.append(node('p','Device status unavailable. Your documents can still sync.'));
    this.progress(this.transfer,false);if(this.syncButton){this.syncButton.disabled=info.running;this.syncButton.textContent=info.phase==='auth'?'Reconnect':'Sync now';}
    // Keep an unfinished nickname edit and its keyboard focus across progress ticks.
    if(!this.deviceList.contains(document.activeElement)){
      this.deviceList.replaceChildren();
      const devices=peers.length?peers:this.data.devices.map(device=>({device,clientKind:'',phase:'unknown',lastSeen:0,sequence:0,outstanding:0,doneBytes:0,totalBytes:0}));
      for(const d of devices){
        const row=node('div',undefined,'sync-device'),heading=node('div',undefined,'sync-device-heading'),name=deviceName(d.device,aliases,devices,self);
        heading.append(node('strong',name+(d.device===self&&name!=='This device'?' · This device':'')),node('small',presence(d,now,info.lastDeviceCheck)));row.append(heading,node('p',receipt(d,info.statusAvailable&&now-info.lastDeviceCheck<45000)));
        if(d.lastSeen)row.append(node('small','Last contact '+new Date(d.lastSeen).toLocaleString()));
        if(info.statusAvailable&&d.phase!=='unknown'&&d.totalBytes)row.append(node('p',`${bytes(d.doneBytes)} of ${bytes(d.totalBytes)} · ${phaseLabel(d.phase)}`));
        const form=node('form'),input=node('input'),save=node('button','Save','quiet');input.setAttribute('aria-label',`Nickname for device ${d.device.slice(0,8)}`);input.placeholder=name;input.value=aliases[d.device]||'';input.maxLength=64;save.type='submit';save.disabled=true;input.oninput=()=>{save.disabled=input.value.trim()===(aliases[d.device]||'')};form.append(input,save);
        form.onsubmit=e=>{e.preventDefault();input.disabled=save.disabled=true;void this.actions.alias(d.device,input.value).catch(()=>{const error=node('p','Could not save this nickname.','sync-error');error.setAttribute('role','alert');row.append(error)}).finally(()=>{input.disabled=false;this.render()})};row.append(form);this.deviceList.append(row);
      }
      if(!devices.length)this.deviceList.append(node('p','No linked device information yet.'));
    }
    this.history.replaceChildren();
    for(const p of activity){const li=node('li');li.append(node('strong',p.name||'Library changes'),node('p',phaseLabel(p.phase)+' · '+(p.direction==='download'?`from ${deviceName(p.source||'',aliases,peers,self)} via server`:'to server')));if(p.publisher&&p.publisher!==p.source)li.append(node('p','Last published by '+deviceName(p.publisher,aliases,peers,self)));this.history.append(li)}
    if(!activity.length)this.history.append(node('li','No transfers in this unlocked session yet.'));
  }
}
