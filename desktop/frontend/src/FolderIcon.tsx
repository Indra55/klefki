import {ChevronDown} from 'lucide-react';
import {useEffect,useRef} from 'react';
import {folderIcon,folderIcons} from '../../../ui/folder-icons';

export function FolderIcon({value}:{value?:string}){
 return <img className="folder-emoji" src={folderIcon(value).src} alt="" aria-hidden="true" draggable={false} width={22} height={22}/>;
}

export function FolderIconPicker({value,onChange}:{value:string;onChange:(value:string)=>void}){
 const details=useRef<HTMLDetailsElement>(null),trigger=useRef<HTMLElement>(null);
 useEffect(()=>{
  const close=(event:KeyboardEvent)=>{if(event.key==='Escape'&&details.current?.open&&details.current.contains(event.target as Node)){event.preventDefault();event.stopPropagation();details.current.open=false;trigger.current?.focus()}};
  // Run before the parent dialog's document capture listener.
  window.addEventListener('keydown',close,true);
  return()=>window.removeEventListener('keydown',close,true);
 },[]);
 return <div className="folder-icon-picker"><span>Icon</span><details ref={details}><summary role="button" aria-label="Choose folder icon" ref={trigger}><FolderIcon value={value}/><span>{folderIcon(value).label}</span><ChevronDown size={14}/></summary><div className="folder-icon-grid" role="group" aria-label="Folder icons">{folderIcons.map(icon=><button type="button" key={icon.value} className="folder-icon-choice" aria-label={icon.label} title={icon.label} aria-pressed={icon.value===value} onClick={()=>{onChange(icon.value);details.current!.open=false;trigger.current?.focus()}}><FolderIcon value={icon.value}/></button>)}</div></details></div>;
}
