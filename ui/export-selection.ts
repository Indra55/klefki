import type {TransferItem} from './library-transfer';

export type ExportEntry={revision:string;name:string;directory:boolean};

function uniqueName(value:string,directory:boolean,used:Set<string>){
 let name=value.normalize('NFC').replace(/[\x00-\x1f\x7f/\\<>:"|?*]/g,'_').trim().replace(/[. ]+$/g,'')||'Untitled';
 if(/^(con|prn|aux|nul|com[1-9]|lpt[1-9])(\.|$)/i.test(name))name='_'+name;
 const index=name.lastIndexOf('.'),base=!directory&&index>0?name.slice(0,index):name,ext=!directory&&index>0?name.slice(index):'';
 for(let n=2;used.has(name.toLowerCase());n++)name=`${base} (${n})${ext}`;
 used.add(name.toLowerCase());return name;
}

export function exportEntries(items:TransferItem[],revisions:string[]):ExportEntry[]{
 if(!revisions.length)throw Error('Select files or folders to download.');
 const byRevision=new Map(items.map(item=>[item.revision,item])),children=new Map<string,TransferItem[]>();
 for(const item of items)if(item.parent){let siblings=children.get(item.parent);if(!siblings){siblings=[];children.set(item.parent,siblings);}siblings.push(item);}
 const roots=revisions.map(revision=>byRevision.get(revision));
 if(roots.some(item=>!item)||roots.some(item=>item!.parent!==roots[0]!.parent))throw Error('Selection changed. Select the items again.');
 const queue=roots.map(item=>({item:item!,parent:''})),seen=new Set<string>(),names=new Map<string,Set<string>>(),entries:ExportEntry[]=[];
 for(let n=0;n<queue.length;n++){
  const {item,parent}=queue[n];
  if(item.trashed||item.conflict||seen.has(item.object)||!['file','folder'].includes(item.type))throw Error('Resolve conflicts before downloading this selection.');
  seen.add(item.object);if(!names.has(parent))names.set(parent,new Set());
  const directory=item.type==='folder',name=parent+uniqueName(item.name,directory,names.get(parent)!)+(directory?'/':'');
  if(new TextEncoder().encode(name).length>65535)throw Error('A selected folder path is too long to export.');
  entries.push({revision:item.revision,name,directory});
  if(directory)for(const child of children.get(item.object)||[])queue.push({item:child,parent:name});
 }
 return entries;
}
