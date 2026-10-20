// Uncompressed ZIP records follow PKWARE APPNOTE 4.3 and 4.5.3.
const limit=0xffffffff;
const table=Uint32Array.from({length:256},(_,n)=>{for(let i=0;i<8;i++)n=n&1?0xedb88320^(n>>>1):n>>>1;return n>>>0;});
const crc32=(bytes:Uint8Array)=>{let crc=limit;for(let i=0;i<bytes.length;i++)crc=table[(crc^bytes[i])&255]^(crc>>>8);return (crc^limit)>>>0;};
function record(length:number,signature:number){const bytes=new Uint8Array(length),view=new DataView(bytes.buffer);view.setUint32(0,signature,true);return {bytes,view};}
function extra(values:number[]){
 const bytes=new Uint8Array(values.length?4+values.length*8:0),view=new DataView(bytes.buffer);
 if(values.length){view.setUint16(0,1,true);view.setUint16(2,values.length*8,true);values.forEach((n,i)=>view.setBigUint64(4+i*8,BigInt(n),true));}
 return bytes;
}

export class ZipArchive {
 private parts:Blob[]=[];
 private central:Blob[]=[];
 private offset=0;
 private count=0;
 add(name:string,bytes:Uint8Array,directory=false){
  const encoded=new TextEncoder().encode(name),size=bytes.length,large=size>=limit,far=this.offset>=limit,version=large||far?45:20;
  if(!encoded.length||encoded.length>65535||!Number.isSafeInteger(this.offset+size+encoded.length+50))throw Error('This selection is too large to export.');
  const crc=crc32(bytes),localExtra=extra(large?[size,size]:[]),local=record(30,0x04034b50);
  local.view.setUint16(4,version,true);local.view.setUint16(6,0x800,true);local.view.setUint16(12,0x21,true);
  local.view.setUint32(14,crc,true);local.view.setUint32(18,large?limit:size,true);local.view.setUint32(22,large?limit:size,true);
  local.view.setUint16(26,encoded.length,true);local.view.setUint16(28,localExtra.length,true);
  const values=[...(large?[size,size]:[]),...(far?[this.offset]:[])],centralExtra=extra(values),central=record(46,0x02014b50);
  central.view.setUint16(4,0x300|version,true);central.view.setUint16(6,version,true);central.view.setUint16(8,0x800,true);central.view.setUint16(14,0x21,true);
  central.view.setUint32(16,crc,true);central.view.setUint32(20,large?limit:size,true);central.view.setUint32(24,large?limit:size,true);
  central.view.setUint16(28,encoded.length,true);central.view.setUint16(30,centralExtra.length,true);
  central.view.setUint32(38,directory?(0o40700<<16)|16:0o100600<<16,true);central.view.setUint32(42,far?limit:this.offset,true);
  const part=new Blob([local.bytes,encoded,localExtra,bytes as Uint8Array<ArrayBuffer>]);
  this.parts.push(part);this.central.push(new Blob([central.bytes,encoded,centralExtra]));this.offset+=part.size;this.count++;
 }
 finish(){
  const directory=new Blob(this.central),large=this.count>=65535||this.offset>=limit||directory.size>=limit,tail:BlobPart[]=[];
  if(large){
   const end=record(56,0x06064b50);end.view.setBigUint64(4,44n,true);end.view.setUint16(12,0x32d,true);end.view.setUint16(14,45,true);
   end.view.setBigUint64(24,BigInt(this.count),true);end.view.setBigUint64(32,BigInt(this.count),true);
   end.view.setBigUint64(40,BigInt(directory.size),true);end.view.setBigUint64(48,BigInt(this.offset),true);
   const locator=record(20,0x07064b50);locator.view.setBigUint64(8,BigInt(this.offset+directory.size),true);locator.view.setUint32(16,1,true);tail.push(end.bytes,locator.bytes);
  }
  const end=record(22,0x06054b50);end.view.setUint16(8,Math.min(this.count,65535),true);end.view.setUint16(10,Math.min(this.count,65535),true);
  end.view.setUint32(12,Math.min(directory.size,limit),true);end.view.setUint32(16,Math.min(this.offset,limit),true);tail.push(end.bytes);
  const archive=new Blob([...this.parts,directory,...tail],{type:'application/zip'});this.clear();return archive;
 }
 clear(){this.parts=[];this.central=[];this.offset=this.count=0;}
}
