import './style.css';
import * as c from './crypto.ts';
import { Store, type Head } from './store.ts';
import * as semantic from './semantic.ts';
import { Remote, sync, HTTPError } from './sync.ts';
const app = document.querySelector<HTMLDivElement>('#app')!;
let account: c.Account | undefined, store: Store | undefined, master: Uint8Array | undefined, credential: Uint8Array | undefined, remote: Remote | undefined, controller: AbortController | undefined;
let modelSetupController:AbortController|undefined;
let generation = 0, busy = false, lastActivity = Date.now(), release: (() => void) | undefined, heads: Head[] = [], parent: string | null = null, filter = '', favorites = false;
const urls = new Set<string>();
const previews = new Set<Uint8Array>();
const node = <K extends keyof HTMLElementTagNameMap>(tag: K, text?: string, className?: string) => { const e = document.createElement(tag); if (text)
    e.textContent = text; if (className)
    e.className = className; return e; };
const button = (text: string, run: () => void, className = '') => { const b = node('button', text, className); b.type = 'button'; b.onclick = run; return b; };
function field(label: string, type = 'text') { const wrap = node('label', label), input = node('input'); input.type = type; wrap.append(input); return { wrap, input }; }
function download(bytes: Uint8Array, name: string, type = 'application/octet-stream') { const url = URL.createObjectURL(new Blob([bytes as Uint8Array<ArrayBuffer>], { type })); urls.add(url); const a = node('a'); a.href = url; a.download = name; a.click(); setTimeout(() => { URL.revokeObjectURL(url); urls.delete(url); }, 10000); }
function message(s: string) { const e = document.querySelector('#status'); if (e)
    e.textContent = s; }
function shell() { app.replaceChildren(); const header = node('header'); header.append(node('a', 'Klefki', 'brand'), node('span', 'Your documents, privately kept.', 'muted')); app.append(header); return app; }
function lock() { modelSetupController?.abort(); modelSetupController=undefined; semantic.clearPrivate(); generation++; controller?.abort(); controller = undefined; remote = undefined; if (master)
    c.wipe(master); if (credential)
    c.wipe(credential); master = credential = undefined; heads = []; store?.close(); store = undefined; release?.(); release = undefined; busy = false; parent = null; filter = ''; favorites = false; for (const url of urls)
    URL.revokeObjectURL(url); urls.clear(); for (const b of previews)
    c.wipe(b); previews.clear(); locked(); }
async function operation(fn: (key: Uint8Array, g: number) => Promise<void>) {
    if (busy || !master)
        return;
    busy = true;
    const g = generation, key = master.slice();
    message('Working…');
    try {
        await fn(key, g);
    }
    catch (e) {
        if (g === generation)
            message(e instanceof HTTPError ? (e.status === 401 ? 'Sign-in expired. Sync again to reconnect.' : 'Server request failed. Your local edits are kept.') : 'Could not complete this action. Check your connection or klefki data.');
        if (e instanceof HTTPError && e.status === 401)
            remote = undefined;
    }
    finally {
        c.wipe(key);
        if (g === generation)
            busy = false;
    }
}
async function claim() {
    c.check(navigator.locks);
    await new Promise<void>((resolve, reject) => {
        void navigator.locks.request('klefki-open-' + account!.account, { ifAvailable: true }, async (held) => { if (!held) {
            reject(Error('Klefki open elsewhere'));
            return;
        } await new Promise<void>(done => { release = done; resolve(); }); }).catch(reject);
    });
}
async function enter(password: string) {
    const unlocked = c.unlock(account!, password);
    master = unlocked.master;
    credential = unlocked.credential;
    try {
        await claim();
        store = await Store.open(account!);
        controller = new AbortController();
        localStorage.setItem('klefki-account', JSON.stringify(account));
        await refresh();
        lastActivity = Date.now();
    }
    catch (e) {
        lock();
        throw e;
    }
}
function locked() {
    const root = shell(), card = node('main', undefined, 'unlock');
    card.append(node('span', 'ENCRYPTED DOCUMENT STORAGE', 'eyebrow'), node('h1', account ? 'Welcome back.' : 'A place for what matters.'), node('p', account ? 'Unlock your local store. Synced documents are available offline.' : 'Create a private space, or open the account you already use on desktop.', 'muted'));
    const form = node('form'), password = field('Password', 'password');
    password.input.required = true;
    password.input.autocomplete = account ? 'current-password' : 'new-password';
    form.append(password.wrap);
    if (account) {
        const go = node('button', 'Unlock Klefki', 'primary');
        go.type = 'submit';
        form.append(go);
        form.onsubmit = async (e) => { e.preventDefault(); if (busy)
            return; busy = true; go.disabled = true; const value = password.input.value; password.input.value = ''; message('Unlocking…'); try {
            await enter(value);
        }
        catch {
            message('Could not unlock. Check the password and close other tabs using this store.');
        }
        finally {
            busy = false;
            go.disabled = false;
        } };
    }
    else {
        const confirm = field('Confirm password', 'password');
        confirm.input.required = true;
        confirm.input.autocomplete = 'new-password';
        form.append(confirm.wrap);
        const go = node('button', 'Create Klefki', 'primary');
        go.type = 'submit';
        form.append(go);
        form.onsubmit = async (e) => {
            e.preventDefault();
            if (busy)
                return;
            if (password.input.value !== confirm.input.value) {
                message('Passwords must match.');
                return;
            }
            busy = true;
            go.disabled = true;
            const value = password.input.value;
            password.input.value = confirm.input.value = '';
            try {
                const created = c.createAccount(value);
                c.wipe(created.master);
                account = created.account;
                await enter(value);
                recoveryDialog(created.recovery);
            }
            catch {
                message('Could not create the store. Browser storage must be available.');
            }
            finally {
                busy = false;
                go.disabled = false;
            }
        };
    }
    card.append(form);
    if (!account) {
        const imp = field('Open an encrypted account file', 'file');
        imp.input.accept = '.klefki,.v1,application/octet-stream';
        imp.input.onchange = async () => { try {
            const file = imp.input.files?.[0];
            if (!file)
                return;
            c.check(file.size === 260);
            account = c.parseAccount(new Uint8Array(await file.arrayBuffer()));
            locked();
        }
        catch {
            message('This is not a valid Klefki account file.');
        } };
        card.append(imp.wrap);
        const id = field('Or enter your account ID');
        id.input.placeholder = '32-character account ID';
        card.append(id.wrap, button('Find account on this server', () => { void (async () => { try {
            c.validID(id.input.value);
            const r = new Remote(location.origin, new AbortController().signal);
            account = c.validateAccount(await r.json<c.Account>('GET', '/v1/accounts/' + id.input.value, undefined, 8192));
            locked();
        }
        catch {
            message('Account unavailable. Check the ID and server connection.');
        } })(); }));
    }
    const status = node('p', '', 'status');
    status.id = 'status';
    status.setAttribute('role', 'status');
    card.append(status, node('p', 'Keys stay in this browser while unlocked. Locking clears the active session.', 'footnote'));
    root.append(card);
}
function recoveryDialog(code: string) { const dialog = node('dialog'); dialog.append(node('h2', 'Save your recovery code'), node('p', 'Store this separately from your private space. It is shown once and is needed if you lose your password.'), node('code', code)); dialog.append(button('Download recovery code', () => download(c.utf8(code + '\n'), 'klefki-recovery.txt', 'text/plain')), button('I have saved it', () => { dialog.close(); dialog.remove(); })); app.append(dialog); dialog.showModal(); dialog.oncancel = e => e.preventDefault(); }
async function refresh() { if (!store || !master)
    return; const g = generation, next = await store.heads(master); if (g !== generation)
    return; heads = next; await library(); }
async function syncNow(register = false) {
    await operation(async (key, g) => {
        const s = store!;
        if (!remote) {
            const r = new Remote(location.origin, controller!.signal);
            await s.bind(r.origin);
            await r.login(account!, credential!, register);
            if (g !== generation)
                return;
            remote = r;
        }
        await sync(s, key, remote);
        if (g !== generation)
            return;
        await refresh();
        message('Synced. All local changes are saved on the server.');
    });
}
function trashed(h: Head, seen = new Set<string>()): boolean { if (h.metadata.tags.includes('klefki:system:trash') && h.metadata.type === 'folder')
    return true; if (!h.metadata.parent || seen.has(h.revision.object))
    return false; seen.add(h.revision.object); return heads.some(p => p.revision.object === h.metadata.parent && trashed(p, seen)); }
async function library() {
    const g = generation, pending = (await store!.all()).filter(x => x.pending).length;
    if (g !== generation)
        return;
    const root = shell(), bar = node('nav');
    bar.append(button('All documents', () => { parent = null; favorites = false; void library(); }), button('Favorites', () => { parent = null; favorites = true; void library(); }), button('Sync now', () => void syncNow(), 'primary'), button('Lock', lock));
    root.append(bar);
    const main = node('main'), top = node('div', undefined, 'title-row');
    top.append(node('h1', favorites ? 'Favorites' : parent ? heads.find(h => h.revision.object === parent)?.metadata.name || 'Folder' : 'Documents'));
    const upload = field('Add document', 'file');
    upload.input.onchange = () => { const f = upload.input.files?.[0]; if (f)
        void operation(async (key, g) => { const bytes = new Uint8Array(await f.arrayBuffer()); try {
            if (g !== generation)
                return;
            await store!.put(key, { type: 'file', name: f.name, parent, tags: [], favorite: false }, bytes);
            if (g === generation) {
                await refresh();
                message('Saved locally. Sync to send encrypted changes.');
            }
        }
        finally {
            c.wipe(bytes);
        } }); };
    top.append(upload.wrap);
    main.append(top);
    const prefs=semantic.preferences(account!.account);
 const search = field(prefs.enabled?'Find your documents':'Search names and tags');
 let ranked:string[]=[],searchSequence=0;
 const searchStatus=node('p','','muted');if(prefs.enabled)searchStatus.setAttribute('role','status');
 const categoryByRevision=new Map<string,string>();
 if(prefs.enabled){const g=generation,key=master!.slice();try{const records=await semantic.corrections(store!,key,heads);if(g!==generation)return;for(const h of heads){const matches=records.filter(r=>r.correction.object===h.revision.object&&r.correction.fileID===h.metadata.file_id);if(matches.length===1)categoryByRevision.set(h.revision.id,matches[0].correction.category)}}catch{searchStatus.textContent='Saved corrections could not be read.'}finally{c.wipe(key)}}
 const category=node('select');category.setAttribute('aria-label','Document category');for(const label of ['All categories',...semantic.categories]){const option=node('option',label);option.value=label==='All categories'?'':label;category.append(option)}category.onchange=()=>renderRows();if(prefs.enabled)main.append(category);
    search.input.value = filter;
    search.input.oninput = () => { filter = search.input.value; void searchRows(); };
    main.append(search.wrap,searchStatus);
    const list = node('div', undefined, 'documents');
    main.append(list);
    function renderRows() {
        list.replaceChildren();
        const visible = heads.filter(h => !semantic.internal(h) && !trashed(h) && (!favorites || h.metadata.favorite) && (favorites || h.metadata.parent === parent) && (!category.value||h.metadata.tags.includes(category.value)||categoryByRevision.get(h.revision.id)===category.value)&&(!filter || ranked.includes(h.revision.id)||(h.metadata.name + ' ' + h.metadata.tags.join(' ')).toLocaleLowerCase().includes(filter.toLocaleLowerCase())));
        if(ranked.length)visible.sort((a,b)=>{const ai=ranked.indexOf(a.revision.id),bi=ranked.indexOf(b.revision.id);return (ai<0?Infinity:ai)-(bi<0?Infinity:bi)});
 if (!visible.length)
            list.append(node('p', 'No documents here yet.', 'empty'));
        for (const h of visible) {
            const row = node('article', undefined, 'document'), label = node('div');
            label.append(node('strong', h.metadata.name), node('p', (h.metadata.type === 'folder' ? 'Folder' : formatSize(h.metadata.size!)) + (h.conflict ? ' · Conflict copy' : '') + (h.metadata.favorite ? ' · Favorite' : '') + (h.metadata.tags.length ? ' · ' + h.metadata.tags.join(', ') : ''), 'muted'));
            row.append(label);
            if (h.metadata.type === 'folder')
                row.append(button('Open folder', () => { parent = h.revision.object; favorites = false; void library(); }));
            else
                row.append(button('Open', () => void openDocument(h)), button('Download', () => void operation(async (key, g) => { const bytes = await store!.export(key, h.revision); try {
                    if (g === generation)
                        download(bytes, h.metadata.name);
                }
                finally {
                    c.wipe(bytes);
                } })));
            if(prefs.enabled&&h.metadata.type==='file')row.append(button('Suggestions',()=>void analysisDialog(h)));
 row.append(button('Edit', () => editDialog(h)));
 const confirmed=categoryByRevision.get(h.revision.id);if(confirmed)label.append(node('small',confirmed));
            list.append(row);
        }
    }
    async function searchRows(){const seq=++searchSequence,q=filter,g=generation;ranked=[];renderRows();if(!prefs.enabled||!q.trim()){searchStatus.textContent='';return}searchStatus.textContent='Searching locally…';await new Promise(resolve=>setTimeout(resolve,200));if(seq!==searchSequence||g!==generation)return;const key=master!.slice();try{const result=await semantic.search(store!,key,heads,q);if(seq!==searchSequence||g!==generation)return;ranked=result.hits.map(h=>h.revision);searchStatus.textContent=result.mode==='hybrid'?'Semantic + keyword search':result.mode==='semantic'?'Semantic search · keyword index unavailable':'Keyword search · '+semanticMessage(result.availability);renderRows()}catch{if(seq===searchSequence&&g===generation)searchStatus.textContent='Local search could not finish. Names and tags still work.'}finally{c.wipe(key)}}
    void searchRows();
    const status = node('p', pending ? `${pending} local change${pending === 1 ? '' : 's'} waiting to sync.` : 'Local store ready.', 'status');
    status.id = 'status';
    status.setAttribute('role', 'status');
    main.append(status);
    const tools = node('details');
    tools.append(node('summary', 'Klefki settings'), node('p', 'Account ID: ' + account!.account, 'account-id'), button('Download encrypted account file', () => download(c.accountBytes(account!), 'account.klefki')), button('Register this store on server', () => void syncNow(true)), button('New folder', () => folderDialog()), node('p', 'Keep the account file and recovery code safe. Browser storage can be cleared or evicted.', 'footnote'));
    addSemanticSettings(tools,prefs);
    main.append(tools);
    root.append(main);
}
const formatSize = (n: number) => n < 1024 ? n + ' B' : n < 1048576 ? (n / 1024).toFixed(1) + ' KB' : (n / 1048576).toFixed(1) + ' MB';
function folderDialog() { const dialog = node('dialog'), f = field('Folder name'); dialog.append(node('h2', 'New folder'), f.wrap, button('Create', () => { if (!f.input.value.trim())
    return; const name = f.input.value; dialog.close(); dialog.remove(); void operation(async (key, g) => { await store!.put(key, { type: 'folder', name, parent, tags: [], favorite: false }); if (g === generation)
    await refresh(); }); }), button('Cancel', () => { dialog.close(); dialog.remove(); })); app.append(dialog); dialog.showModal(); }
function editDialog(h: Head) {
    const d = node('dialog'), name = field('Name'), tags = field('Tags, separated by commas'), favorite = field('Favorite', 'checkbox');
    name.input.value = h.metadata.name;
    tags.input.value = h.metadata.tags.join(', ');
    favorite.input.checked = h.metadata.favorite;
    d.append(node('h2', 'Edit document'), name.wrap, tags.wrap, favorite.wrap);
    let text: HTMLTextAreaElement | undefined;
    if (h.metadata.type === 'file' && /\.(txt|md|csv|json)$/i.test(h.metadata.name) && h.metadata.size! <= 1048576) {
        d.append(button('Edit text content', () => void operation(async (key, g) => { const b = await store!.export(key, h.revision); try {
            if (g !== generation)
                return;
            text = node('textarea');
            text.setAttribute('aria-label', 'Document text');
            text.value = c.decode(b);
            d.append(text);
        }
        finally {
            c.wipe(b);
        } })));
    }
    d.append(button('Save changes', () => {
        const n = name.input.value, t = tags.input.value.split(',').map(x => x.trim()).filter(Boolean);
        if (!n.trim() || n.length > 255 || /[\x00/\\]/.test(n) || t.some(x => x.startsWith('klefki:system:')))
            return;
        const edit = { name: n, parent: h.metadata.parent, tags: t, favorite: favorite.input.checked }, content = text?.value;
        d.close();
        d.remove();
        void operation(async (key, g) => { if (content !== undefined) {
            const bytes = c.utf8(content);
            try {
                await store!.put(key, { ...h.metadata, ...edit }, bytes, h.revision);
            }
            finally {
                c.wipe(bytes);
            }
        }
        else
            await store!.edit(key, h.revision, edit); if (g === generation) {
            await refresh();
            message('Changes saved locally.');
        } });
    }), button('Cancel', () => { d.close(); d.remove(); }));
    app.append(d);
    d.showModal();
}
async function openDocument(h: Head) {
    await operation(async (key, g) => {
        const b = await store!.export(key, h.revision);
        try {
            if (g !== generation) {
                c.wipe(b);
                return;
            }
            previews.add(b);
            const d = node('dialog', undefined, 'preview');
            d.append(node('h2', h.metadata.name));
            let url: string | undefined;
            if (/\.(txt|md|csv|json)$/i.test(h.metadata.name)) {
                d.append(node('pre', c.decode(b)));
            }
            else if (/\.(png|jpe?g|gif|webp)$/i.test(h.metadata.name)) {
                url = URL.createObjectURL(new Blob([b as Uint8Array<ArrayBuffer>]));
                urls.add(url);
                const img = node('img');
                img.src = url;
                img.alt = h.metadata.name;
                d.append(img);
            }
            else
                d.append(node('p', 'This file is ready to download.'), button('Download file', () => download(b, h.metadata.name)));
            const close = () => { if (url) {
                URL.revokeObjectURL(url);
                urls.delete(url);
            } c.wipe(b); previews.delete(b); d.remove(); };
            d.append(button('Close', () => d.close()));
            d.onclose = close;
            app.append(d);
            d.showModal();
            // Non-preview types need their bytes only until the dialog is closed.
            if (!/\.(txt|md|csv|json|png|jpe?g|gif|webp)$/i.test(h.metadata.name))
                return;
        }
        finally {
            if (/\.(txt|md|csv|json|png|jpe?g|gif|webp)$/i.test(h.metadata.name))
                c.wipe(b);
        }
    });
}
await c.ready;
try {
    const saved = localStorage.getItem('klefki-account');
    if (saved)
        account = c.validateAccount(JSON.parse(saved));
}
catch {
    account = undefined;
}
locked();
for (const event of ['pointerdown', 'keydown'])
    document.addEventListener(event, () => { lastActivity = Date.now(); });
setInterval(() => { if (master && Date.now() - lastActivity > 5 * 60 * 1000)
    lock(); }, 1000);
window.addEventListener('online', () => { if (master && !busy)
    void syncNow(); });
window.addEventListener('pagehide', lock);
if ('serviceWorker' in navigator)
    void navigator.serviceWorker.register('/sw.js').catch(() => message('Offline app installation is unavailable in this browser.'));

function semanticMessage(code:string){return ({SEARCH_REBUILD_REQUIRED:'Sync a current index from desktop or CLI.',SEMANTIC_UNAVAILABLE:'The local model is unavailable. Enable it in settings.',SEMANTIC_MODEL_INVALID:'Model verification or inference failed.',SEMANTIC_CORRECTION_CONFLICT:'Choose a correction to resolve changes from different devices.',SEARCH_INDEX_INVALID:'The synced index is invalid. Rebuild it on desktop or CLI.',SEARCH_LIMIT:'The query exceeds local search limits.'} as Record<string,string>)[code]||'Local semantic search is unavailable.'}
function addSemanticSettings(parent:HTMLElement,prefs:semantic.Preferences){
 const section=node('section'),enable=field('Semantic search and suggestions on this device','checkbox'),feedback=node('p','Optional. Processing stays on this device. One-time download: about '+Math.ceil(semantic.downloadBytes/1048576)+' MB. Desktop or CLI must build and sync the document index.','muted');enable.input.checked=prefs.enabled;let setupController:AbortController|undefined;const cancel=button('Cancel setup',()=>{setupController?.abort();semantic.clearPrivate()});cancel.hidden=true;
 async function setup(bundle?:File[]){const g=generation;setupController=new AbortController();modelSetupController=setupController;cancel.hidden=false;enable.input.disabled=true;install.disabled=true;try{await semantic.setup(setupController.signal,(done,total)=>{if(g===generation)feedback.textContent='Downloading local model: '+Math.round(done/total*100)+'%'},bundle);if(g!==generation)return;prefs.enabled=true;semantic.savePreferences(account!.account,prefs);await library()}catch(e){if(g===generation){enable.input.checked=false;feedback.textContent=semanticMessage(e instanceof Error?e.message:'SEMANTIC_UNAVAILABLE')}}finally{enable.input.disabled=false;install.disabled=false;cancel.hidden=true;setupController=undefined}}
 enable.input.onchange=()=>{if(enable.input.checked)void setup();else{prefs.enabled=false;semantic.savePreferences(account!.account,prefs);setupController?.abort();semantic.clearPrivate();void library()}};
 const install=button('Install BGE-small model',()=>void setup());
 const bundle=field('Select verified local model bundle','file');bundle.input.multiple=true;bundle.input.setAttribute('webkitdirectory','');bundle.input.onchange=()=>void setup(Array.from(bundle.input.files||[]));
 const cutoff=field('Classification similarity cutoff','number'),margin=field('Winning category margin','number');cutoff.input.value=String(prefs.cutoff);cutoff.input.min='-1';cutoff.input.max='1';cutoff.input.step='.01';margin.input.value=String(prefs.margin);margin.input.min='0';margin.input.max='2';margin.input.step='.01';const thresholds=node('details');thresholds.append(node('summary','Classification thresholds'),cutoff.wrap,margin.wrap,button('Save thresholds',()=>{const a=Number(cutoff.input.value),b=Number(margin.input.value);if(!Number.isFinite(a)||!Number.isFinite(b)||a<-1||a>1||b<0||b>2)return;prefs.cutoff=a;prefs.margin=b;semantic.savePreferences(account!.account,prefs);feedback.textContent='Classification thresholds saved on this device.'}));section.append(node('h3','Semantic search and suggestions'),node('p','Model: BGE-small-en-v1.5.','muted'),feedback,enable.wrap,install,cancel,bundle.wrap,thresholds);parent.append(section)
}
async function analysisDialog(h:Head){const g=generation,key=master!.slice();try{const a=await semantic.analyze(store!,key,heads,h,semantic.preferences(account!.account));if(g!==generation)return;const dialog=node('dialog'),select=node('select');select.setAttribute('aria-label','Suggested category');for(const label of ['',...semantic.categories]){const option=node('option',label||'Uncategorized');option.value=label;select.append(option)}select.value=a.category;const title=node('p',a.confirmed?'Your confirmed category.':a.category?'Suggested category: '+a.category:'No confident category suggestion.');dialog.append(node('h2','Local suggestions'),title,select);const fields=node('div');function showFields(){fields.replaceChildren();for(const f of a.fields){const input=field(f.kind==='date'?'Date':'Amount'+(f.currency?' ('+f.currency+')':''));input.input.value=f.value||f.raw;input.input.oninput=()=>{f.value=input.input.value};fields.append(input.wrap,node('small',f.evidence))}}showFields();dialog.append(fields);if(a.conflicts.length>1){dialog.append(node('p','Competing corrections: select one, review its fields, then confirm.'));for(const r of a.conflicts)dialog.append(button('Use '+(r.correction.category||'uncategorized')+' · '+r.revision.slice(0,6),()=>{select.value=r.correction.category;a.fields=r.correction.fields;showFields()}))}dialog.append(button(a.conflicts.length?'Resolve with reviewed values':'Confirm category and fields',()=>{const category=select.value;dialog.close();dialog.remove();void operation(async(key,current)=>{await semantic.correct(store!,key,heads,h,category,a.fields,a.conflicts.map(r=>r.revision));if(current===generation)await refresh()})}),button('Close',()=>{dialog.close();dialog.remove()}));app.append(dialog);dialog.showModal()}catch(e){if(g===generation)message(semanticMessage(e instanceof Error?e.message:'SEMANTIC_UNAVAILABLE'))}finally{c.wipe(key)}}
