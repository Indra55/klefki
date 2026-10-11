import '@fontsource/inter/latin-400.css';
import '@fontsource/inter/latin-500.css';
import './style.css';
import { node, button, field, icon, iconButton, menu, preferences } from './ui.ts';
import { settingsPage, type Section } from './settings.ts';
import { showPreview, closePreviews } from './preview.ts';
import * as c from './crypto.ts';
import { Store, unlockRevision, type Head } from './store.ts';
import * as semantic from './semantic.ts';
import * as passkey from './passkey.ts';
import { Remote, sync, setSyncMode, HTTPError, RelayWaiting } from './sync.ts';
const app = document.querySelector<HTMLDivElement>('#app')!;
let account: c.Account | undefined, store: Store | undefined, master: Uint8Array | undefined, credential: Uint8Array | undefined, remote: Remote | undefined, controller: AbortController | undefined;
let modelSetupController:AbortController|undefined;
let passkeyController: AbortController | undefined;
let generation = 0, busy = false, lastActivity = Date.now(), release: (() => void) | undefined, heads: Head[] = [], parent: string | null = null, filter = '', favorites = false;
const urls = new Set<string>();
const previews = new Set<Uint8Array>();
let waitingUpdate: ServiceWorkerRegistration | undefined, applyingUpdate = false;
let view = 'All documents', tag = '', sort = preferences().defaultSort, settings: Section | null = null, feedback = '', syncState = 'Local only';
function download(bytes: Uint8Array, name: string, type = 'application/octet-stream') { const url = URL.createObjectURL(new Blob([bytes as Uint8Array<ArrayBuffer>], { type })); urls.add(url); const a = node('a'); a.href = url; a.download = name; a.click(); setTimeout(() => { URL.revokeObjectURL(url); urls.delete(url); }, 10000); }
function message(text: string) {
    feedback = text;
    const status = document.querySelector('#status'); if (status) status.textContent = text;
}
function syncStatus(text: string) {
    syncState = text;
    const label = document.querySelector('[aria-label="Sync status"]');
    if (label?.firstChild) label.firstChild.textContent = text;
}
function showSettings(section: Section = 'appearance') { settings = section; void library().then(() => document.querySelector<HTMLHeadingElement>('.settings-heading h1')?.focus()); }
function navigate(next: string) { settings = null; view = next; parent = null; tag = ''; filter = ''; void library(); }
function shell() {
    app.replaceChildren();
    const root = node('div', undefined, 'shell'), header = node('header', undefined, 'topbar');
    header.append(button('Klefki', () => { if (master) navigate('All documents'); }, 'brand'));
    if (master) {
        const actions = node('div', undefined, 'top-actions');
        const syncMenu = menu(syncState, [{label: 'Sync now', run: () => void syncNow()}, {label: 'Connection settings', run: () => showSettings('sync')}]);
        syncMenu.querySelector('summary')!.setAttribute('aria-label', 'Sync status');
        const setting = iconButton('Settings', 'settings', () => showSettings(settings || 'appearance'), 'quiet navbar-settings'); setting.setAttribute('aria-pressed', String(!!settings));
        const locking = button('Lock',lock,'quiet lock'); locking.append(icon('lock',14));
        actions.append(syncMenu, setting, locking); header.append(actions);
    }
    root.append(header); if (waitingUpdate?.waiting) offerUpdate(root); app.append(root); return root;
}
function lock() { closePreviews(); passkeyController?.abort(); passkeyController = undefined; modelSetupController?.abort(); modelSetupController=undefined; semantic.clearPrivate(); generation++; controller?.abort(); controller = undefined; remote = undefined; if (master)
    c.wipe(master); if (credential)
    c.wipe(credential); master = credential = undefined; heads = []; store?.close(); store = undefined; release?.(); release = undefined; busy = false; parent = null; filter = ''; favorites = false; view = 'All documents'; tag = ''; settings = null; feedback = ''; syncState = 'Local only'; for (const url of urls)
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
            message(e instanceof RelayWaiting ? e.message : e instanceof HTTPError ? (e.status === 401 ? 'Sign-in expired. Sync again to reconnect.' : e.status===507 ? 'Relay storage is full. Your local edits are kept; sync the other linked devices to free space.' : e.status===428 ? 'This account uses temporary relay. Update this client to sync.' : 'Server request failed. Your local edits are kept.') : 'Could not complete this action. Check your connection or klefki data.');
        if (e instanceof HTTPError && e.status === 401) remote = undefined;
        if (g === generation && syncState === 'Syncing') syncStatus(e instanceof RelayWaiting ? 'Waiting for another device' : e instanceof HTTPError && e.status === 401 ? 'Authentication required' : 'Sync error');
    }
    finally {
        c.wipe(key);
        if (g === generation)
            busy = false;
    }
}
async function claim(target: c.Account) {
    c.check(navigator.locks);
    return new Promise<() => void>((resolve, reject) => {
        void navigator.locks.request('klefki-open-' + target.account, { ifAvailable: true }, async (held) => { if (!held) {
            reject(Error('Klefki open elsewhere'));
            return;
        } await new Promise<void>(done => resolve(done)); }).catch(reject);
    });
}
async function enter(password: string) {
    const target = account!, g = generation;
    await enterKeys(target, c.unlock(target, password), g);
}
async function enterKeys(target: c.Account, unlocked: passkey.Keys, g: number) {
    let held: (() => void) | undefined, opened: Store | undefined, adopted = false;
    try {
        c.check(g === generation && account === target);
        held = await claim(target);
        c.check(g === generation && account === target);
        opened = await Store.open(target);
        c.check(g === generation && account === target);
        master = unlocked.master;
        credential = unlocked.credential;
        store = opened;
        release = held;
        adopted = true;
        controller = new AbortController();
        localStorage.setItem('klefki-account', JSON.stringify(target));
        await refresh();
        if (g === generation) lastActivity = Date.now();
    }
    catch (e) {
        if (adopted && g === generation) lock();
        throw e;
    }
    finally {
        if (!adopted) { c.wipe(unlocked.master); c.wipe(unlocked.credential); opened?.close(); held?.(); }
    }
}
function locked() {
    const root = shell(), card = node('main', undefined, 'setup unlock');
    card.append(node('span', 'ENCRYPTED DOCUMENT STORAGE', 'eyebrow'), node('h1', account ? 'Welcome back.' : 'A place for what matters.'), node('p', account ? 'Unlock your local store. Synced documents are available offline.' : 'Create a private space, or open the account you already use on desktop.', 'muted'));
    const form = node('form', undefined, 'setup-form'), password = field('Password', 'password');
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
    if (account && passkey.configured(account)) {
        const target = account;
        const go = button('Unlock with passkey', () => void (async () => {
            if (busy) return;
            const g = generation, pending = new AbortController();
            busy = true; go.disabled = true; cancel.hidden = false; passkeyController = pending;
            message('Verify with your fingerprint, face, or device PIN…');
            try { await enterKeys(target, await passkey.unlock(target, pending.signal), g); }
            catch (error) { if (g === generation) message(passkey.errorMessage(error)); }
            finally {
                if (passkeyController === pending) passkeyController = undefined;
                if (g === generation) { busy = false; go.disabled = false; cancel.hidden = true; }
            }
        })(), 'passkey-unlock');
        const cancel = button('Cancel passkey verification', () => passkeyController?.abort());
        cancel.hidden = true;
        card.append(go, cancel, node('p', 'Your device chooses fingerprint, face recognition, or PIN. Password unlock is always available.', 'muted'));
    }
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
function recoveryDialog(code: string) { const dialog = node('dialog',undefined,'modal'); dialog.append(node('h2', 'Save your recovery code'), node('p', 'Store this separately from your private space. It is shown once and is needed if you lose your password.'), node('code', code)); dialog.append(button('Download recovery code', () => download(c.utf8(code + '\n'), 'klefki-recovery.txt', 'text/plain')), button('I have saved it', () => { dialog.close(); dialog.remove(); })); app.append(dialog); dialog.showModal(); dialog.oncancel = e => e.preventDefault(); }
async function refresh() { if (!store || !master)
    return; const g = generation, next = await store.heads(master); if (g !== generation)
    return; heads = next; await library(); }
async function connectRemote(register=false){
    const s=store!, r=new Remote(location.origin,controller!.signal);
    await s.bind(r.origin);
    await r.login(account!,credential!,register,await s.state('sync-mode')==='temporary-relay');
    remote=r;
}
async function syncNow(register = false) {
    if (busy || !master) return;
    syncStatus('Syncing');
    await operation(async (key, g) => {
        const s = store!;
        if (!remote) {
            await connectRemote(register);
            if (g !== generation)
                return;
        }
        try { await sync(s,key,remote!); }
        catch(e){if(!(e instanceof HTTPError)||e.status!==401||await s.state('sync-mode')!=='temporary-relay')throw e;await connectRemote();await sync(s,key,remote!);}
        if (g !== generation)
            return;
        syncStatus('Synced');
        await refresh();
        if(await s.state('sync-mode')==='temporary-relay'){
            const status=await remote!.json<{queued:number}>('GET','/v1/sync/settings',undefined,1024);
            syncStatus(status.queued ? 'Waiting for linked devices' : 'Synced');
            message(status.queued?'Uploaded to the relay. Waiting for linked devices to save their copies.':'Synced. Delivered documents have been removed from the relay.');
        }else message('Synced. All local changes are saved on the server.');
    });
}
function trashed(h: Head, seen = new Set<string>()): boolean { if (h.metadata.tags.includes('klefki:system:trash') && h.metadata.type === 'folder')
    return true; if (!h.metadata.parent || seen.has(h.revision.object))
    return false; seen.add(h.revision.object); return heads.some(p => p.revision.object === h.metadata.parent && trashed(p, seen)); }
async function library() {
    if (!store || !master) return;
    const g = generation, s = store;
    const pending = (await s.all()).filter(x => x.pending).length;
    const prefs = semantic.preferences(account!.account);
    const mode = await s.state<string>('sync-mode') || 'persistent';
    const devices = await s.state<string[]>('relay-devices') || [], device = await s.state<string>('relay-device');
    const categoryByRevision = new Map<string, string>();
    if (prefs.enabled) {
        const key = master.slice();
        try {
            const records = await semantic.corrections(s, key, heads);
            for (const h of heads) {
                const matches = records.filter(r => r.correction.object === h.revision.object && r.correction.fileID === h.metadata.file_id);
                if (matches.length === 1) categoryByRevision.set(h.revision.id, matches[0].correction.category);
            }
        } catch { /* Names and tags remain searchable without a usable index. */ }
        finally { c.wipe(key); }
    }
    if (g !== generation || store !== s) return;
    const root = shell();
    const status = node('p', feedback, 'status');
    status.id = 'status'; status.setAttribute('role', 'status'); root.append(status);
    if (settings) {
        root.append(settingsPage({
            section: settings, account: account!.account, pending, mode, devices, device,
            changeSection: showSettings, back: () => { settings = null; void library(); },
            sync: register => void syncNow(register), exportAccount: () => download(c.accountBytes(account!), 'account.klefki'),
            setMode: mode => void operation(async (key, current) => {
                if (!remote) await connectRemote();
                await setSyncMode(s, key, remote!, mode as 'persistent' | 'temporary-relay');
                if (current === generation) { await refresh(); message('Sync mode updated for this account.'); }
            }),
            forget: id => void operation(async (key, current) => {
                if (!remote) await connectRemote();
                await remote!.request('DELETE', '/v1/sync/relay/devices/' + id, undefined, 1024);
                await sync(s, key, remote!);
                if (current === generation) { await refresh(); message('Device removed from relay deliveries.'); }
            }),
            addPasskey: addPasskeySettings, addSemantic: panel => addSemanticSettings(panel, prefs),
            preferenceChanged: () => { sort = preferences().defaultSort; void library(); message('Preferences saved on this device.'); },
        }));
        root.querySelector('.settings-panel')?.append(status);
        return;
    }
    const main = node('main', undefined, 'library'); main.dataset.density = preferences().density;
    const heading = node('h1', 'Documents', 'sr-only'); main.append(heading);
    const searchWrap = node('div', undefined, 'search-wrap'), search = node('input', undefined, 'search');
    search.type = 'search'; search.placeholder = 'Find your documents'; search.autocomplete = 'off'; search.value = filter;
    search.setAttribute('aria-label', prefs.enabled ? 'Find your documents' : 'Search names and tags');
    searchWrap.append(search, node('span', 'Ctrl K', 'search-hint'));
    const clear = iconButton('', 'close', () => { filter = ''; search.value = ''; void searchRows(); }, 'icon'); clear.setAttribute('aria-label', 'Clear search'); clear.hidden = !filter; searchWrap.append(clear);
    main.append(searchWrap);
    const searchTools = node('div', undefined, 'search-tools'), searchStatus = node('span', '', 'muted small');
    const index = button('Search index settings', () => showSettings('search'), 'quiet'); searchTools.append(searchStatus, index); main.append(searchTools);
    const categories = node('nav', undefined, 'categories'); categories.setAttribute('aria-label', 'Document categories');
    const tags = [...semantic.categories, ...Array.from(new Set(heads.filter(h => !trashed(h)).flatMap(h => h.metadata.tags))).filter(t => !semantic.categories.includes(t) && !t.startsWith('klefki:system:')).sort()];
    for (const label of ['All', ...tags]) {
        const item = button(label, () => { tag = label === 'All' ? '' : label; void library(); }, 'category' + ((label === 'All' ? !tag : label === tag) ? ' selected' : ''));
        item.setAttribute('aria-pressed', String(label === 'All' ? !tag : label === tag)); categories.append(item);
    }
    const categoryLine = node('div', undefined, 'category-line'), actions = node('div', undefined, 'library-actions');
    const upload = node('input'); upload.type = 'file'; upload.multiple = true; upload.hidden = true; upload.setAttribute('aria-label', 'Add document');
    upload.onchange = () => { void addFiles(Array.from(upload.files || [])); upload.value = ''; };
    actions.append(iconButton('New folder', 'folderPlus', folderDialog), iconButton('Add files', 'plus', () => upload.click(), 'add-files'), upload);
    categoryLine.append(categories); if (view !== 'Trash') categoryLine.append(actions); main.append(categoryLine);
    const tools = node('div', undefined, 'library-tools'), crumbs = node('div', undefined, 'breadcrumbs');
    crumbs.append(menu(view, ['All documents','Favorites','Folders','Trash'].map(label => ({label,run: () => navigate(label)}))));
    if (view === 'Folders') {
        const chain: Head[] = [], seen = new Set<string>(); let ancestor = heads.find(h => h.revision.object === parent);
        while (ancestor && !seen.has(ancestor.revision.object)) { seen.add(ancestor.revision.object); chain.unshift(ancestor); ancestor = heads.find(h => h.revision.object === ancestor!.metadata.parent); }
        for (const h of chain) { const crumb = node('span'); crumb.append(icon('chevron',12), button(h.metadata.name, () => { parent = h.revision.object; void library(); }, 'quiet')); crumbs.append(crumb); }
    }
    const right = node('div', undefined, 'library-tools-right'), sorting = node('select'); sorting.setAttribute('aria-label','Sort documents');
    for (const [value,label] of [['name','Name'],['type','Type'],['size','Size']]) { const option = node('option',label); option.value = value; sorting.append(option); }
    sorting.value = sort; sorting.onchange = () => { sort = sorting.value as typeof sort; renderRows(); };
    const organize = button('Organize files',() => {},'quiet'); organize.disabled = true; organize.title = 'Review file organization in the desktop app, then sync the tags here.'; if (view !== 'Trash') right.append(organize);
    const sortLabel = node('label',undefined,'sort'); sortLabel.append(sorting); right.append(sortLabel); tools.append(crumbs,right); main.append(tools,status);
    if (view === 'Trash') main.append(node('p','Documents stay here until you restore them. Nothing is permanently deleted.','trash-note'));
    const list = node('div', undefined, 'documents'), footer = node('footer', undefined, 'list-footer'); list.setAttribute('aria-label','Documents'); main.append(list,footer); root.append(main);
    let ranked: string[] = [], searchSequence = 0;
    function renderRows() {
        list.replaceChildren(); clear.hidden = !filter;
        const visible = heads.filter(h => !semantic.internal(h) && !isTrashRoot(h) && (view === 'Trash' ? trashed(h) : !trashed(h)) && (view !== 'Favorites' || h.metadata.favorite) && (view !== 'Folders' || h.metadata.parent === parent) && (!tag || h.metadata.tags.includes(tag) || categoryByRevision.get(h.revision.id) === tag) && (!filter || ranked.includes(h.revision.id) || (h.metadata.name + ' ' + h.metadata.tags.join(' ')).toLocaleLowerCase().includes(filter.toLocaleLowerCase())));
        visible.sort((a,b) => {
            if (ranked.length) { const ai = ranked.indexOf(a.revision.id), bi = ranked.indexOf(b.revision.id); const rank = (ai < 0 ? Infinity : ai) - (bi < 0 ? Infinity : bi); if (rank) return rank; }
            return (sort === 'size' ? (b.metadata.size || 0) - (a.metadata.size || 0) : sort === 'type' ? a.metadata.type.localeCompare(b.metadata.type) : 0) || a.metadata.name.localeCompare(b.metadata.name);
        });
        for (const h of visible) {
            const row = node('article', undefined, 'document'), open = button('', () => {
                if (h.metadata.type === 'folder' && !trashed(h)) { view = 'Folders'; parent = h.revision.object; filter = ''; void library(); }
                else if (h.metadata.type === 'file') void openDocument(h);
            }, 'document-open');
            open.setAttribute('aria-label', 'Open ' + h.metadata.name);
            const title = node('div', undefined, 'document-title'); if (h.metadata.type === 'folder') title.append(icon('folder',18)); title.append(node('span',h.metadata.name));
            if (h.conflict) title.append(node('span','Conflict copy · ' + h.revision.id.slice(0,6),'conflict'));
            const meta = node('div', undefined, 'document-meta'), tags = h.metadata.tags.filter(t => !t.startsWith('klefki:system:'));
            meta.append(node('span',tags.join(' · ') || (h.metadata.type === 'folder' ? 'Folder' : h.metadata.name.split('.').pop()?.toUpperCase())));
            if (h.metadata.type === 'file') meta.append(node('span','·','meta-dot'), node('span',formatSize(h.metadata.size!)));
            if (h.metadata.favorite) meta.append(icon('star',12,true)); open.append(title,meta); row.append(open);
            const actions = node('div', undefined, 'row-actions');
            if (trashed(h)) actions.append(button('Restore', () => restoreDocument(h), 'quiet'));
            else {
                const star = iconButton('', 'star', () => toggleFavorite(h), 'icon favorite' + (h.metadata.favorite ? ' is-favorite' : ''));
                star.setAttribute('aria-label', (h.metadata.favorite ? 'Unfavorite ' : 'Favorite ') + h.metadata.name); if (h.metadata.favorite) star.replaceChildren(icon('star',17,true)); actions.append(star);
            }
            const choices: {label: string; run: () => void; danger?: boolean}[] = [];
            if (h.metadata.type === 'file') choices.push({label:'Export',run: () => exportDocument(h)});
            if (trashed(h)) choices.push({label:'Restore',run: () => restoreDocument(h)});
            else {
                choices.push({label:'Edit details & move',run: () => editDialog(h)}, {label:h.metadata.favorite ? 'Remove from favorites' : 'Add to favorites',run: () => toggleFavorite(h)});
                if (prefs.enabled && h.metadata.type === 'file') choices.push({label:'Suggestions',run: () => void analysisDialog(h)});
                choices.push({label:'Move to Trash',run: () => trashDocument(h),danger:true});
            }
            actions.append(menu('Actions for ' + h.metadata.name,choices,'more')); row.append(actions); list.append(row);
        }
        if (!visible.length) {
            const empty = node('div',undefined,'empty'); empty.append(icon('search',25),node('h2',filter ? 'Nothing matches yet.' : view === 'Trash' ? 'Your trash is empty.' : view === 'Favorites' ? 'Keep the important ones close.' : tag ? 'A place for ' + tag.toLowerCase() + '.' : 'A little room for what matters.'),node('p',filter ? 'Try another name, tag, or indexed word.' : view === 'Trash' ? 'Documents you move here can always be restored.' : view === 'Favorites' ? 'Star a document to find it here.' : 'Add your first document, or drop files anywhere in this window.')); list.append(empty);
        }
        footer.hidden = !visible.length; footer.replaceChildren(document.createTextNode(visible.length + (visible.length === 1 ? ' item' : ' items')),node('span','Stored encrypted · available offline'));
    }
    async function searchRows() {
        const sequence = ++searchSequence, query = filter; ranked = []; renderRows();
        if (!prefs.enabled || !query.trim()) { searchStatus.textContent = ''; return; }
        searchStatus.textContent = 'Searching locally…'; await new Promise(resolve => setTimeout(resolve,200));
        if (sequence !== searchSequence || g !== generation || !searchStatus.isConnected) return;
        const key = master!.slice();
        try {
            const result = await semantic.search(s,key,heads,query);
            if (sequence !== searchSequence || g !== generation || !searchStatus.isConnected) return;
            ranked = result.hits.map(h => h.revision);
            searchStatus.textContent = result.mode === 'hybrid' ? 'Semantic + keyword search' : result.mode === 'semantic' ? 'Semantic search · keyword index unavailable' : 'Keyword search · ' + semanticMessage(result.availability); renderRows();
        } catch { if (sequence === searchSequence && g === generation) searchStatus.textContent = 'Local search could not finish. Names and tags still work.'; }
        finally { c.wipe(key); }
    }
    search.oninput = () => { filter = search.value; void searchRows(); }; void searchRows();
    main.ondragover = event => { event.preventDefault(); };
    main.ondrop = event => { event.preventDefault(); if (view !== 'Trash') void addFiles(Array.from(event.dataTransfer?.files || [])); };
}
const formatSize = (n: number) => n < 1024 ? n + ' B' : n < 1048576 ? (n / 1024).toFixed(0) + ' KB' : (n / 1048576).toFixed(1) + ' MB';
async function addFiles(files: File[]) {
    for (const file of files) {
        if (!master) break;
        await operation(async (key,g) => {
            const bytes = new Uint8Array(await file.arrayBuffer());
            try {
                if (g !== generation) return;
                await store!.put(key,{type:'file',name:file.name,parent:view === 'Folders' ? parent : null,tags:tag ? [tag] : [],favorite:false},bytes);
                if (g === generation) { await refresh(); message('Saved locally. Sync to send encrypted changes.'); }
            } finally { c.wipe(bytes); }
        });
    }
}
function exportDocument(h: Head) {
    void operation(async (key,g) => { const bytes = await store!.export(key,h.revision); try { if (g === generation) download(bytes,h.metadata.name); } finally { c.wipe(bytes); } });
}
function toggleFavorite(h: Head) {
    void operation(async (key,g) => { await store!.edit(key,h.revision,{...h.metadata,favorite:!h.metadata.favorite}); if (g === generation) { await refresh(); message('Changes saved locally.'); } });
}
function isTrashRoot(h: Head) { return h.metadata.type === 'folder' && h.metadata.tags.includes('klefki:system:trash'); }
function validParent(object: string, destination: string | null) {
    const seen = new Set([object]); let current = destination;
    while (current) {
        if (seen.has(current)) return false; seen.add(current);
        const folder = heads.find(h => h.revision.object === current && h.metadata.type === 'folder' && !h.conflict && !trashed(h));
        if (!folder) return false; current = folder.metadata.parent;
    }
    return true;
}
function trashDocument(h: Head) {
    void operation(async (key,g) => {
        let trash = heads.find(isTrashRoot);
        if (!trash) {
            const revision = await store!.put(key,{type:'folder',name:'Trash',parent:null,tags:['klefki:system:trash'],favorite:false});
            trash = {revision,metadata:{type:'folder',name:'Trash',parent:null,tags:['klefki:system:trash'],favorite:false},conflict:false};
        }
        await store!.edit(key,h.revision,{...h.metadata,parent:trash.revision.object});
        if (g === generation) { await refresh(); message('Document moved to Trash.'); }
    });
}
function restoreDocument(h: Head) {
    void operation(async (key,g) => {
        let destination: string | null = null;
        if (heads.some(folder => isTrashRoot(folder) && folder.revision.object === h.metadata.parent) && h.revision.base) {
            const base = await store!.revision(h.revision.base), previous = unlockRevision(key,account!.account,base);
            destination = previous.metadata.parent; if (previous.key) c.wipe(previous.key);
        }
        if (!validParent(h.revision.object,destination)) destination = null;
        await store!.edit(key,h.revision,{...h.metadata,parent:destination});
        if (g === generation) { await refresh(); message('Document restored.'); }
    });
}
function folderDialog() {
    const dialog = node('dialog',undefined,'modal'), input = field('Folder name'); input.input.required = true; input.input.maxLength = 255;
    const form = node('form'); form.append(input.wrap);
    const create = node('button','Create folder','primary'); create.type = 'submit';
    const actions = node('div',undefined,'form-actions'); actions.append(button('Cancel',() => dialog.close(),'quiet'),create); form.append(actions);
    form.onsubmit = event => {
        event.preventDefault(); const name = input.input.value.trim(); if (!name || /[\x00/\\]/.test(name)) return;
        dialog.close(); dialog.remove(); void operation(async (key,g) => { await store!.put(key,{type:'folder',name,parent:view === 'Folders' ? parent : null,tags:tag ? [tag] : [],favorite:false}); if (g === generation) await refresh(); });
    };
    dialog.append(node('h2','A new folder'),form); dialog.onclose = () => dialog.remove(); app.append(dialog); dialog.showModal();
}
function editDialog(h: Head) {
    const d = node('dialog', undefined, 'modal'), name = field('Name'), tags = field('Tags, separated by commas'), favorite = field('Favorite', 'checkbox');
    name.input.value = h.metadata.name;
    tags.input.value = h.metadata.tags.join(', ');
    favorite.input.checked = h.metadata.favorite;
    const folderLabel = node('label', 'Folder'), destination = node('select'); destination.setAttribute('aria-label','Folder');
    const root = node('option', 'Library root'); root.value = ''; destination.append(root);
    for (const folder of heads.filter(item => item.metadata.type === 'folder' && !trashed(item) && !item.conflict && validParent(h.revision.object,item.revision.object))) {
        const option = node('option',folder.metadata.name); option.value = folder.revision.object; destination.append(option);
    }
    destination.value = h.metadata.parent || ''; folderLabel.append(destination);
    d.append(node('h2', 'Document details'), name.wrap, tags.wrap, folderLabel, favorite.wrap);
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
        if (!validParent(h.revision.object,destination.value || null)) return;
        const edit = { name: n.trim(), parent: destination.value || null, tags: Array.from(new Set(t)), favorite: favorite.input.checked }, content = text?.value;
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
    await operation(async (key,g) => {
        const bytes = await store!.export(key,h.revision);
        if (g !== generation) { c.wipe(bytes); return; }
        previews.add(bytes);
        try {
            await showPreview(h.metadata.name,bytes,() => { c.wipe(bytes); previews.delete(bytes); },() => download(bytes,h.metadata.name));
        } catch { c.wipe(bytes); previews.delete(bytes); throw Error('Preview unavailable'); }
        if (g === generation) message('');
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
setInterval(() => { if (master && Date.now() - lastActivity > preferences().idleMinutes * 60 * 1000)
    lock(); }, 1000);
window.addEventListener('online', () => { if (master && !busy)
    void syncNow(); });
window.addEventListener('pagehide', lock);
document.addEventListener('keydown', event => {
    if (!master || !(event.ctrlKey || event.metaKey)) return;
    if (event.shiftKey && event.key.toLowerCase() === 'l') { event.preventDefault(); lock(); }
    else if (event.key.toLowerCase() === 'k') {
        event.preventDefault(); settings = null;
        void library().then(() => document.querySelector<HTMLInputElement>('.search')?.focus());
    }
});
function offerUpdate(root: HTMLElement) {
    if (!waitingUpdate?.waiting || root.querySelector('.app-update')) return;
    const banner = node('div',undefined,'app-update');
    banner.append(node('span','A Klefki update is ready.'), button('Update and reopen',() => {
        const worker = waitingUpdate?.waiting; if (!worker) return;
        applyingUpdate = true; lock(); worker.postMessage('ACTIVATE_UPDATE');
    },'secondary'));
    root.prepend(banner);
}
if ('serviceWorker' in navigator) {
    const announce = (registration: ServiceWorkerRegistration) => {
        if (!registration.waiting || !navigator.serviceWorker.controller) return;
        waitingUpdate = registration;
        const root = document.querySelector<HTMLElement>('.shell'); if (root) offerUpdate(root);
    };
    navigator.serviceWorker.addEventListener('controllerchange',() => { if (applyingUpdate) location.reload(); });
    void navigator.serviceWorker.register('/sw.js').then(registration => {
        announce(registration);
        registration.addEventListener('updatefound',() => {
            const worker = registration.installing;
            worker?.addEventListener('statechange',() => { if (worker.state === 'installed') announce(registration); });
        });
    }).catch(() => message('Offline app installation is unavailable in this browser.'));
}

function semanticMessage(code:string){return ({SEARCH_REBUILD_REQUIRED:'Sync a current index from desktop or CLI.',SEMANTIC_UNAVAILABLE:'The local model is unavailable. Enable it in settings.',SEMANTIC_MODEL_INVALID:'Model verification or inference failed.',SEMANTIC_CORRECTION_CONFLICT:'Choose a correction to resolve changes from different devices.',SEARCH_INDEX_INVALID:'The synced index is invalid. Rebuild it on desktop or CLI.',SEARCH_LIMIT:'The query exceeds local search limits.'} as Record<string,string>)[code]||'Local semantic search is unavailable.'}
function addPasskeySettings(parent: HTMLElement) {
    const target = account!, g = generation, section = node('section');
    const feedback = node('p', passkey.configured(target) ? 'Passkey unlock is enabled in this browser.' : 'Enable device verification to unlock without typing your password.', 'muted');
    feedback.setAttribute('aria-live', 'polite');
    const enable = button('Enable passkey unlock', () => void (async () => {
        if (busy || !master || !credential) return;
        const pending = new AbortController();
        busy = true; passkeyController = pending; enable.disabled = true;
        cancel.hidden = false; disable.disabled = true;
        feedback.textContent = 'Create a passkey, then verify it to finish setup. Your device may ask twice.';
        try {
            await passkey.enable(target, { master, credential }, pending.signal);
            if (g !== generation) return;
            feedback.textContent = 'Passkey unlock is enabled in this browser.';
            enable.hidden = true; disable.hidden = false;
        }
        catch (error) { if (g === generation) feedback.textContent = passkey.errorMessage(error); }
        finally {
            if (passkeyController === pending) passkeyController = undefined;
            if (g === generation) { busy = false; enable.disabled = false; cancel.hidden = true; disable.disabled = false; }
        }
    })());
    const cancel = button('Cancel passkey setup', () => passkeyController?.abort());
    cancel.hidden = true;
    const disable = button('Remove passkey unlock', () => {
        if (busy) return;
        try {
            passkey.remove(target); enable.hidden = false; disable.hidden = true;
            feedback.textContent = 'Passkey unlock was removed from this browser. You can also delete the passkey in your device’s credential manager.';
        }
        catch { feedback.textContent = 'Browser storage could not be updated. Try removing passkey unlock again.'; }
    });
    enable.hidden = passkey.configured(target); disable.hidden = !enable.hidden;
    section.append(node('h3', 'Passkey unlock'), feedback, enable, cancel, disable, node('p', 'Optional fingerprint, face, or PIN verification. Setup is saved only in this browser; your passkey provider may sync the passkey. Keep your password and recovery code. Clearing site data removes this setup.', 'footnote'));
    parent.append(section);
    void passkey.available().then(available => {
        if (g !== generation || !section.isConnected || available) return;
        enable.disabled = true;
        feedback.textContent = passkey.errorMessage(new Error('PASSKEY_UNAVAILABLE'));
    });
}
function addSemanticSettings(parent:HTMLElement,prefs:semantic.Preferences){
 const section=node('section',undefined,'settings-section'),enable=field('Semantic search and suggestions on this device','checkbox'),feedback=node('p','Optional. Processing stays on this device. One-time download: about '+Math.ceil(semantic.downloadBytes/1048576)+' MB. Desktop or CLI must build and sync the document index.','muted');enable.input.checked=prefs.enabled;let setupController:AbortController|undefined;const cancel=button('Cancel setup',()=>{setupController?.abort();semantic.clearPrivate()});cancel.hidden=true;
 async function setup(bundle?:File[]){const g=generation;setupController=new AbortController();modelSetupController=setupController;cancel.hidden=false;enable.input.disabled=true;install.disabled=true;try{await semantic.setup(setupController.signal,(done,total)=>{if(g===generation)feedback.textContent='Downloading local model: '+Math.round(done/total*100)+'%'},bundle);if(g!==generation)return;prefs.enabled=true;semantic.savePreferences(account!.account,prefs);await library()}catch(e){if(g===generation){enable.input.checked=false;feedback.textContent=semanticMessage(e instanceof Error?e.message:'SEMANTIC_UNAVAILABLE')}}finally{enable.input.disabled=false;install.disabled=false;cancel.hidden=true;setupController=undefined}}
 enable.input.onchange=()=>{if(enable.input.checked)void setup();else{prefs.enabled=false;semantic.savePreferences(account!.account,prefs);setupController?.abort();semantic.clearPrivate();void library()}};
 const install=button('Install BGE-small model',()=>void setup());
 const bundle=field('Select verified local model bundle','file');bundle.input.multiple=true;bundle.input.setAttribute('webkitdirectory','');bundle.input.onchange=()=>void setup(Array.from(bundle.input.files||[]));
 const cutoff=field('Classification similarity cutoff','number'),margin=field('Winning category margin','number');cutoff.input.value=String(prefs.cutoff);cutoff.input.min='-1';cutoff.input.max='1';cutoff.input.step='.01';margin.input.value=String(prefs.margin);margin.input.min='0';margin.input.max='2';margin.input.step='.01';const thresholds=node('details');thresholds.append(node('summary','Classification thresholds'),cutoff.wrap,margin.wrap,button('Save thresholds',()=>{const a=Number(cutoff.input.value),b=Number(margin.input.value);if(!Number.isFinite(a)||!Number.isFinite(b)||a<-1||a>1||b<0||b>2)return;prefs.cutoff=a;prefs.margin=b;semantic.savePreferences(account!.account,prefs);feedback.textContent='Classification thresholds saved on this device.'}));section.append(node('h3','Semantic search and suggestions'),node('p','Model: BGE-small-en-v1.5.','muted'),feedback,enable.wrap,install,cancel,bundle.wrap,thresholds);parent.append(section)
}
async function analysisDialog(h:Head){const g=generation,key=master!.slice();try{const a=await semantic.analyze(store!,key,heads,h,semantic.preferences(account!.account));if(g!==generation)return;const dialog=node('dialog',undefined,'modal'),select=node('select');select.setAttribute('aria-label','Suggested category');for(const label of ['',...semantic.categories]){const option=node('option',label||'Uncategorized');option.value=label;select.append(option)}select.value=a.category;const title=node('p',a.confirmed?'Your confirmed category.':a.category?'Suggested category: '+a.category:'No confident category suggestion.');dialog.append(node('h2','Local suggestions'),title,select);const fields=node('div');function showFields(){fields.replaceChildren();for(const f of a.fields){const input=field(f.kind==='date'?'Date':'Amount'+(f.currency?' ('+f.currency+')':''));input.input.value=f.value||f.raw;input.input.oninput=()=>{f.value=input.input.value};fields.append(input.wrap,node('small',f.evidence))}}showFields();dialog.append(fields);if(a.conflicts.length>1){dialog.append(node('p','Competing corrections: select one, review its fields, then confirm.'));for(const r of a.conflicts)dialog.append(button('Use '+(r.correction.category||'uncategorized')+' · '+r.revision.slice(0,6),()=>{select.value=r.correction.category;a.fields=r.correction.fields;showFields()}))}dialog.append(button(a.conflicts.length?'Resolve with reviewed values':'Confirm category and fields',()=>{const category=select.value;dialog.close();dialog.remove();void operation(async(key,current)=>{await semantic.correct(store!,key,heads,h,category,a.fields,a.conflicts.map(r=>r.revision));if(current===generation)await refresh()})}),button('Close',()=>{dialog.close();dialog.remove()}));app.append(dialog);dialog.showModal()}catch(e){if(g===generation)message(semanticMessage(e instanceof Error?e.message:'SEMANTIC_UNAVAILABLE'))}finally{c.wipe(key)}}
