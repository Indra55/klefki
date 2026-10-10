import {readdir,readFile,writeFile} from 'node:fs/promises';
import {createHash} from 'node:crypto';
const root=new URL('../dist/',import.meta.url);
const names=(await readdir(root,{recursive:true})).filter(n=>/\.(js|css|html|svg|png|webmanifest)$/.test(n)&&n!=='sw.js'&&!n.startsWith('models/'));
const hash=createHash('sha256');for(const name of names)hash.update(await readFile(new URL(name,root)));
const model=JSON.parse(await readFile(new URL('../src/model-manifest.json',import.meta.url)));
const cache='klefki-shell-'+hash.digest('hex').slice(0,16),assets=names.map(n=>n==='index.html'?'/':'/'+n);
await writeFile(new URL('sw.js',root),`const CACHE=${JSON.stringify(cache)},ASSETS=${JSON.stringify(assets)},MODEL_CACHE=${JSON.stringify("klefki-model-"+model.fingerprint)};
self.addEventListener('install',event=>event.waitUntil(caches.open(CACHE).then(cache=>cache.addAll(ASSETS))));
self.addEventListener('activate',event=>event.waitUntil(caches.keys().then(keys=>Promise.all(keys.filter(k=>k.startsWith('klefki-shell-')&&k!==CACHE).map(k=>caches.delete(k)))).then(()=>self.clients.claim())));
self.addEventListener('fetch',event=>{
 const url=new URL(event.request.url);
 if(event.request.method!=='GET'||url.origin!==self.location.origin||url.search||url.pathname.startsWith('/v1/'))return;
 const path=url.pathname;
 if(path.startsWith('/models/')){event.respondWith(caches.open(MODEL_CACHE).then(async cache=>(await cache.match(path))||fetch(event.request)));return;}
 if(!ASSETS.includes(path))return;
 event.respondWith(caches.open(CACHE).then(async cache=>(await cache.match(path))||fetch(event.request)));
});
`);
