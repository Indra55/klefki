import {node,button} from './ui.ts';
import type {DocumentEvent} from './store.ts';

export class IncomingView {
 private trigger?:HTMLButtonElement;
 private dialog?:HTMLDialogElement;
 private events:DocumentEvent[]=[];
 private toast?:HTMLElement;
 private timer?:ReturnType<typeof setTimeout>;
 private pending:DocumentEvent[]=[];
 private batch?:ReturnType<typeof setTimeout>;
 constructor(private actions:{open:(revision:string)=>void;read:()=>Promise<void>}){}
 mount(parent:HTMLElement){this.trigger=button('',()=>this.show(),'incoming-trigger');parent.append(this.trigger);this.update(this.events);}
 update(events:DocumentEvent[]){
  this.events=events;const count=events.filter(x=>!x.read).length;
  if(this.trigger){this.trigger.setAttribute('aria-label','Incoming documents'+(count?`, ${count} unread`:''));this.trigger.title='Incoming documents';this.trigger.replaceChildren(node('span','↓'),...(count?[node('span',String(count),'incoming-count')]:[]));}
 }
 clear(){clearTimeout(this.timer);clearTimeout(this.batch);this.pending=[];this.events=[];this.toast?.remove();this.toast=undefined;this.dialog?.close();this.dialog?.remove();this.dialog=undefined;this.trigger=undefined;}
 ready(event:DocumentEvent){
  if(this.pending.some(x=>x.revision===event.revision))return;
  this.pending.push(event);clearTimeout(this.batch);this.batch=setTimeout(()=>this.announce(),350);
 }
 private announce(){
  const events=this.pending.splice(0);if(!events.length)return;
  this.toast?.remove();clearTimeout(this.timer);
  const container=node('div',undefined,'notifications incoming-notifications'),toast=node('div',undefined,'incoming-toast');toast.setAttribute('aria-live','polite');
  toast.append(node('span',events.length===1?`${events[0].name} is ready`:`${events.length} new documents are ready`),button(events.length===1?'Open':'View',()=>{container.remove();if(events.length===1)this.actions.open(events[0].revision);else this.show()},'quiet'));
  container.append(toast);document.body.append(container);this.toast=container;this.timer=setTimeout(()=>container.remove(),7000);
  if(document.visibilityState!=='visible'||!document.hasFocus())void this.systemNotification(events);
 }
 private async systemNotification(events:DocumentEvent[]){
  if(!('Notification' in window)||Notification.permission!=='granted'||localStorage.getItem('klefki-incoming-notifications')!=='on')return;
  try{const registration=await navigator.serviceWorker.ready;await registration.showNotification('Klefki',{body:events.length===1?'A new document is ready in your vault.':`${events.length} new documents are ready in your vault.`,tag:'klefki-incoming',data:{revision:events.length===1?events[0].revision:undefined}});}catch{}
 }
 private show(){
  this.dialog?.remove();const dialog=node('dialog',undefined,'sync-panel incoming-panel');dialog.setAttribute('aria-label','Incoming documents');
  const header=node('header');header.append(node('h2','Incoming documents'),button('Close',()=>dialog.close(),'quiet'));dialog.append(header);
  if(!this.events.length)dialog.append(node('p','New documents from your other devices will appear here.'));
  const list=node('ul',undefined,'sync-history');
  for(const event of this.events){const row=node('li');row.append(node('strong',event.name),node('small',new Date(event.created).toLocaleString()),button('Open '+event.name,()=>{dialog.close();this.actions.open(event.revision)},'quiet'));list.append(row);}
  dialog.append(list);
  if('Notification' in window){const toggle=button(localStorage.getItem('klefki-incoming-notifications')==='on'&&Notification.permission==='granted'?'Disable system notifications':'Enable system notifications',()=>{
   if(localStorage.getItem('klefki-incoming-notifications')==='on'){localStorage.removeItem('klefki-incoming-notifications');toggle.textContent='Enable system notifications';return;}
   void Notification.requestPermission().then(permission=>{if(permission==='granted'){localStorage.setItem('klefki-incoming-notifications','on');toggle.textContent='Disable system notifications';}else{toggle.textContent='System notifications unavailable';toggle.disabled=true;}}).catch(()=>{toggle.textContent='System notifications unavailable';toggle.disabled=true;});
  },'quiet');dialog.append(toggle);}
  dialog.append(node('p','System notifications use a generic message. Receiving pauses when the vault locks; browsers may suspend background pages.'));
  dialog.onclose=()=>{dialog.remove();if(this.dialog===dialog)this.dialog=undefined;this.trigger?.focus()};document.body.append(dialog);this.dialog=dialog;dialog.showModal();void this.actions.read();
 }
}
