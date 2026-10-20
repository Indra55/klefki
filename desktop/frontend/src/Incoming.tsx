import {useEffect,useRef,useState} from 'react';
import {Bell} from 'lucide-react';
import {api,type IncomingDocument} from './api';
import {Modal} from './ui';

export function Incoming({events,event,generation,onOpen,onRefresh}:{events:IncomingDocument[];event?:IncomingDocument|null;generation:number;onOpen:(revision:string)=>void;onRefresh:()=>Promise<void>}){
 const [open,setOpen]=useState(false),[toast,setToast]=useState<IncomingDocument[]>([]),[systemAvailable,setSystemAvailable]=useState(false);
 useEffect(()=>{void api().SystemNotificationsAvailable?.().then(setSystemAvailable).catch(()=>{});},[]);
 const pending=useRef<IncomingDocument[]>([]),count=events.filter(x=>!x.read).length;
 useEffect(()=>{
  if(!event)return;
  if(!pending.current.some(x=>x.revision===event.revision))pending.current.push(event);
  let hide:number|undefined;
  const batch=window.setTimeout(()=>{const incoming=pending.current.splice(0);setToast(incoming);hide=window.setTimeout(()=>setToast([]),7000);if(!document.hasFocus()||document.visibilityState!=='visible')void api().NotifyIncoming?.().catch(()=>{});},350);
  return()=>{window.clearTimeout(batch);window.clearTimeout(hide);};
 },[event,generation]);
 return <><button className="incoming-trigger" aria-label={`Incoming documents${count?`, ${count} unread`:''}`} title="Incoming documents" onClick={()=>{setOpen(true);void api().ReadIncoming?.().then(onRefresh)}}><Bell size={17}/>{count>0&&<span className="incoming-count">{count}</span>}</button>
 {open&&<Modal title="Incoming documents" onClose={()=>setOpen(false)}><ul className="sync-history">{events.length?events.map(event=><li key={event.revision}><strong>{event.name}</strong><small>{new Date(event.created).toLocaleString()}</small><button className="quiet" onClick={()=>{setOpen(false);onOpen(event.revision)}}>Open {event.name}</button></li>):<li>New documents from your other devices will appear here.</li>}</ul><p>{systemAvailable?'System notifications show a generic message while this window is in the background.':'System notifications are unavailable in this environment; incoming activity is saved here.'} Locking or minimizing the vault pauses receiving.</p></Modal>}
 {toast.length>0&&<div className="notifications incoming-notifications"><div className="incoming-toast" aria-live="polite"><span>{toast.length===1?`${toast[0].name} is ready`:`${toast.length} new documents are ready`}</span><button className="quiet" onClick={()=>{if(toast.length===1)onOpen(toast[0].revision);else setOpen(true);setToast([])}}>{toast.length===1?'Open':'View'}</button></div></div>}
 </>;
}
