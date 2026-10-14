import {folderIcon,folderIcons} from '../../ui/folder-icons.ts';
import {button,icon,node} from './ui.ts';

export function folderImage(value?:string){
 const image=node('img',undefined,'folder-emoji');image.src=folderIcon(value).src;image.alt='';image.setAttribute('aria-hidden','true');image.draggable=false;image.width=image.height=22;return image;
}

export function folderIconPicker(value='',change:(value:string)=>void){
 const wrap=node('div',undefined,'folder-icon-picker'),details=node('details'),trigger=node('summary'),label=node('span'),choices=node('div',undefined,'folder-icon-grid');
 trigger.setAttribute('role','button');trigger.setAttribute('aria-label','Choose folder icon');
 choices.setAttribute('role','group');choices.setAttribute('aria-label','Folder icons');
 details.onkeydown=event=>{if(event.key==='Escape'){event.preventDefault();event.stopPropagation();details.open=false;trigger.focus()}};
 const image=folderImage(value);trigger.append(image,label,icon('down',14));
 function update(){image.src=folderIcon(value).src;label.textContent=folderIcon(value).label;for(const choice of choices.querySelectorAll('button'))choice.setAttribute('aria-pressed',String(choice.dataset.icon===value));}
 for(const item of folderIcons){const choice=button('',()=>{value=item.value;change(value);update();details.open=false;trigger.focus()},'folder-icon-choice');choice.dataset.icon=item.value;choice.setAttribute('aria-label',item.label);choice.title=item.label;choice.append(folderImage(item.value));choices.append(choice);}
 details.append(trigger,choices);wrap.append(node('span','Icon'),details);update();return wrap;
}
