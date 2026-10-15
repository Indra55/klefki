import {node,button} from './ui.ts';
import {idleSync,syncLabel,syncIndicator,syncLegend,activityPage,phaseLabel,deviceName,transferDetail,receipt,presence,devicePendingDetail,waitingDevices,bytes,TransferRate,type SyncInfo,type SyncActivity,type DeviceStatus} from '../../ui/sync-status.ts';

type Data = {info:SyncInfo;pending:number;server:string;aliases:Record<string,string>;self:string;activity:SyncActivity[];lastSync:number;devices:string[]};
type Actions = {pause:()=>void;refresh:()=>void;settings:()=>void;panel:(open:boolean)=>void;alias:(id:string,name:string)=>Promise<void>};
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
  private pagination?:HTMLElement;
  private previous?:HTMLButtonElement;
  private next?:HTMLButtonElement;
  private pageLabel?:HTMLElement;
  private page=0;
  private rate=new TransferRate();
  constructor(private actions:Actions) {}
  get open() { return !!this.dialog?.open; }
  mount(actions:HTMLElement,root:HTMLElement) {
    this.trigger=button('',()=>this.show(),'sync-trigger');this.trigger.setAttribute('aria-label','Sync status');this.trigger.setAttribute('aria-haspopup','dialog');this.trigger.setAttribute('aria-expanded',String(this.open));
    const dot=node('span',undefined,'sync-dot');dot.setAttribute('aria-hidden','true');this.label=node('span',undefined,'sr-only');this.label.setAttribute('aria-live','polite');this.label.setAttribute('aria-atomic','true');this.trigger.append(dot,this.label);actions.append(this.trigger);
    this.strip=node('section',undefined,'sync-strip');this.strip.setAttribute('aria-label','Sync progress');root.append(this.strip);
    this.render();
  }
  update(data:Data) { this.data=data;this.render(); }
  clear() { this.dialog?.close();this.dialog?.remove();this.dialog=undefined;this.trigger=undefined;this.strip=undefined;this.page=0;this.rate=new TransferRate();this.data={info:idleSync(),pending:0,server:'',aliases:{},self:'',activity:[],lastSync:0,devices:[]}; }
  private show() {
    if (!this.dialog) {
      const dialog=node('dialog',undefined,'sync-panel');dialog.setAttribute('aria-label','Sync activity');
      const header=node('header');header.append(node('h2','Sync activity'),button('Close',()=>dialog.close(),'quiet'));
      this.summary=node('div');this.transfer=node('section',undefined,'sync-strip');
      const controls=node('div',undefined,'sync-controls');this.syncButton=button('Refresh sync',()=>this.actions.refresh(),'quiet');this.syncButton.dataset.syncStart='';const pause=button('Pause sync',()=>this.actions.pause(),'quiet');pause.dataset.syncPause='';controls.append(this.syncButton,pause,button('Connection settings',()=>{dialog.close();this.actions.settings()},'quiet'));
      const legend=node('div',undefined,'sync-legend');legend.setAttribute('aria-label','Sync color legend');
      for(const item of syncLegend){const entry=node('span');entry.dataset.state=item.state;const dot=node('span',undefined,'sync-dot');dot.setAttribute('aria-hidden','true');entry.append(dot,document.createTextNode(item.label));legend.append(entry)}
      this.deviceList=node('div',undefined,'sync-devices');this.history=node('ul',undefined,'sync-history');
      this.pagination=node('nav',undefined,'sync-pagination');this.pagination.setAttribute('aria-label','Recent activity pages');
      this.previous=button('Previous',()=>{this.page--;this.render()},'quiet');this.next=button('Next',()=>{this.page++;this.render()},'quiet');this.pageLabel=node('span');this.pageLabel.setAttribute('aria-live','polite');this.pagination.append(this.previous,this.pageLabel,this.next);
      dialog.append(header,this.summary,this.transfer,controls,legend,node('h3','Linked devices'),this.deviceList,node('p','Nicknames are saved only on this device.'),node('h3','Recent activity'),this.history,this.pagination);
      dialog.onclose=()=>{this.actions.panel(false);this.trigger?.setAttribute('aria-expanded','false');this.trigger?.focus()};
      dialog.onclick=e=>{if(e.target===dialog){const rect=dialog.getBoundingClientRect();if(e.clientX<rect.left||e.clientX>rect.right||e.clientY<rect.top||e.clientY>rect.bottom)dialog.close()}};
      document.body.append(dialog);this.dialog=dialog;
    }
    if(!this.open)this.page=0;this.dialog.showModal();this.trigger?.setAttribute('aria-expanded','true');this.actions.panel(true);this.render();
  }
  private progress(container:HTMLElement,details:boolean) {
    const {info,aliases,self}=this.data,p=info.progress;
    container.hidden=!info.running&&!info.paused;
    if (!info.running) {
      if(info.paused&&container.dataset.paused!=='true'){container.dataset.paused='true';const copy=node('div');copy.append(node('strong',syncLabel(info,self,aliases)),node('p','Local changes stay queued until you resume.'));container.replaceChildren(copy);if(details){const resume=button('Resume sync',()=>this.actions.pause(),'quiet');resume.dataset.syncPause='';container.append(resume);}}
      return;
    }
    if(container.dataset.paused){container.replaceChildren();delete container.dataset.paused;}
    let text=container.querySelector<HTMLDivElement>('.sync-copy');
    if(!text){text=node('div',undefined,'sync-copy');text.append(node('strong'),node('p',undefined,'sync-detail'),node('p',undefined,'sync-speed'));container.append(text)}
    text.querySelector('strong')!.textContent=info.paused?syncLabel(info,self,aliases):phaseLabel(info.phase);
    text.querySelector('.sync-detail')!.textContent=p?transferDetail(p,aliases,info.peers?.devices||[],self):'Connecting to your server…';
    const speed=p?this.rate.update(p):'',speedNode=text.querySelector<HTMLElement>('.sync-speed')!;speedNode.textContent=speed;speedNode.hidden=!speed;
    if(details&&!container.querySelector('button'))container.append(button('Details',()=>this.show(),'quiet'));
    let progress=container.querySelector('progress');if(!progress){progress=node('progress');progress.setAttribute('aria-label','Document sync progress');container.append(progress)}
    progress.max=p?.totalBytes||1;if(p?.totalBytes&&p.phase!=='verifying')progress.value=p.doneBytes;else progress.removeAttribute('value');
  }
  private render() {
    const {info,self,aliases,pending,server,lastSync,activity}=this.data,now=Date.now(),peers=info.peers?.devices||[];
    const {label,state}=syncIndicator(info,self,aliases,pending,now);
    if(this.label)this.label.textContent=label;
    if(this.trigger){this.trigger.title=label;this.trigger.dataset.state=state;this.trigger.setAttribute('aria-description',label);}
    if(this.strip)this.progress(this.strip,true);
    if(!this.open||!this.summary||!this.deviceList||!this.history||!this.transfer)return;
    this.summary.replaceChildren(node('p',label,state==='error'?'sync-error':undefined),node('p',server,'sync-address'),node('p',pending?`${pending} changes waiting to upload.`:'No local changes waiting to upload.'));
    const waiting=waitingDevices(info,self,now);if(!info.running&&waiting.length&&info.waitingFor!=='source')this.summary.append(node('p',`Saved on server; waiting for ${waiting.map(d=>deviceName(d.device,aliases,peers,self)).join(', ')}.`));
    if(info.lastContact)this.summary.append(node('p','Last successful server contact '+new Date(info.lastContact).toLocaleString()));
    if(lastSync)this.summary.append(node('p','Last sync completed '+new Date(lastSync).toLocaleString()));
    if(!info.statusAvailable)this.summary.append(node('p','Device status unavailable. Your documents can still sync.'));
    for(const pause of this.dialog!.querySelectorAll<HTMLButtonElement>('[data-sync-pause]'))pause.textContent=info.paused?'Resume sync':'Pause sync';
    this.progress(this.transfer,false);if(this.syncButton)this.syncButton.disabled=info.running||!!info.paused;
    // Keep an unfinished nickname edit and its keyboard focus across progress ticks.
    if(!this.deviceList.contains(document.activeElement)){
      this.deviceList.replaceChildren();
      const devices:DeviceStatus[]=peers.length?peers:this.data.devices.map(device=>({device,clientKind:'',phase:'unknown',lastSeen:0,sequence:0,outstanding:0,doneBytes:0,totalBytes:0}));
      for(const d of devices){
        const row=node('div',undefined,'sync-device'),heading=node('div',undefined,'sync-device-heading'),name=deviceName(d.device,aliases,devices,self);
        heading.append(node('strong',name+(d.device===self&&name!=='This device'?' · This device':'')),node('small',presence(d,now,info.lastDeviceCheck)));row.append(heading,node('p',receipt(d,info.statusAvailable&&now-info.lastDeviceCheck<45000)));
        const pendingDetail=devicePendingDetail(d,info.statusAvailable&&now-info.lastDeviceCheck<45000);if(pendingDetail)row.append(node('p',pendingDetail));
        if(d.deviceState)row.append(node('small','Pending-state report '+new Date(d.deviceState.reportedAt).toLocaleString()));
        if(d.lastSeen)row.append(node('small','Last contact '+new Date(d.lastSeen).toLocaleString()));
        if(info.statusAvailable&&d.phase!=='unknown'&&d.totalBytes)row.append(node('p',`${bytes(d.doneBytes)} of ${bytes(d.totalBytes)} · ${phaseLabel(d.phase)}`));
        const form=node('form'),input=node('input'),save=node('button','Save','quiet');input.setAttribute('aria-label',`Nickname for device ${d.device.slice(0,8)}`);input.placeholder=name;input.value=aliases[d.device]||'';input.maxLength=64;save.type='submit';save.disabled=true;input.oninput=()=>{save.disabled=input.value.trim()===(aliases[d.device]||'')};form.append(input,save);
        form.onsubmit=e=>{e.preventDefault();input.disabled=save.disabled=true;void this.actions.alias(d.device,input.value).catch(()=>{const error=node('p','Could not save this nickname.','sync-error');error.setAttribute('role','alert');row.append(error)}).finally(()=>{input.disabled=false;this.render()})};row.append(form);this.deviceList.append(row);
      }
      if(!devices.length)this.deviceList.append(node('p','No linked device information yet.'));
    }
    this.history.replaceChildren();
    const page=activityPage(activity,this.page);this.page=page.page;
    for(const p of page.items){const li=node('li');li.append(node('strong',p.name||'Library changes'),node('p',phaseLabel(p.phase)+' · '+(p.direction==='download'?`from ${deviceName(p.source||'',aliases,peers,self)} via server`:'to server')));if(p.publisher&&p.publisher!==p.source)li.append(node('p','Last published by '+deviceName(p.publisher,aliases,peers,self)));this.history.append(li)}
    if(!activity.length)this.history.append(node('li','No transfers in this unlocked session yet.'));
    if(this.pagination)this.pagination.hidden=page.pages===1;
    if(this.previous)this.previous.disabled=page.page===0;
    if(this.next)this.next.disabled=page.page===page.pages-1;
    if(this.pageLabel)this.pageLabel.textContent=`Page ${page.page+1} of ${page.pages}`;
  }
}
