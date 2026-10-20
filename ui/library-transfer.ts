export type TransferItem = {object:string;revision:string;name:string;type:string;parent:string|null;conflict:boolean;trashed:boolean};

export function transferDestinations(items:TransferItem[], selected:TransferItem[]) {
 const source=selected[0]?.parent;
 const blocked=new Set(selected.filter(i=>i.type==='folder').map(i=>i.object));
 const valid=(folder:TransferItem)=>{
  const seen=new Set(blocked);let current:TransferItem|undefined=folder;
  while(current){
   if(seen.has(current.object)||current.trashed||current.conflict)return false;
   seen.add(current.object);
   if(current.parent===null)return true;
   current=items.find(i=>i.object===current!.parent&&i.type==='folder');
  }
  return false;
 };
 const path=(folder:TransferItem)=>{
  const names=[folder.name];let current=folder;
  while(current.parent){current=items.find(i=>i.object===current.parent&&i.type==='folder')!;names.unshift(current.name)}
  return 'All / '+names.join(' / ');
 };
 return [
  ...(source!==null?[{value:'',label:'Library root'}]:[]),
  ...items.filter(i=>i.type==='folder'&&i.object!==source&&valid(i)).map(i=>({value:i.object,label:path(i)})).sort((a,b)=>a.label.localeCompare(b.label))
 ];
}

export function transferTree(items:TransferItem[], revisions:string[], destination:string|null, copy:boolean) {
 const roots=revisions.map(revision=>items.find(i=>i.revision===revision));
 if(!roots.length||roots.some(i=>!i||i.trashed||i.conflict)||new Set(roots.map(i=>i!.object)).size!==roots.length)throw Error('Selection changed. Select the items again.');
 const selected=roots as TransferItem[];
 if(selected.some(i=>i.parent!==selected[0].parent)||!transferDestinations(items,selected).some(d=>d.value===(destination||'')))throw Error('Choose another destination folder.');
 const plan=[...selected];
 if(copy)for(let n=0;n<plan.length;n++){
  if(plan[n].type!=='folder')continue;
  for(const child of items.filter(i=>i.parent===plan[n].object)){
   if(child.trashed||child.conflict||plan.some(i=>i.object===child.object))throw Error('Resolve folder conflicts before copying.');
   plan.push(child);
  }
 }
 return plan;
}
