import './style.css';
import * as c from './crypto.ts';
import { Store, type Head } from './store.ts';
import { Remote, sync, HTTPError } from './sync.ts';
const app = document.querySelector<HTMLDivElement>('#app')!;
let account: c.Account | undefined, store: Store | undefined, master: Uint8Array | undefined, credential: Uint8Array | undefined, remote: Remote | undefined, controller: AbortController | undefined;
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
function shell() { app.replaceChildren(); const header = node('header'); header.append(node('a', 'vault', 'brand'), node('span', 'Your documents, privately kept.', 'muted')); app.append(header); return app; }
function lock() { generation++; controller?.abort(); controller = undefined; remote = undefined; if (master)
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
            message(e instanceof HTTPError ? (e.status === 401 ? 'Sign-in expired. Sync again to reconnect.' : 'Server request failed. Your local edits are kept.') : 'Could not complete this action. Check your connection or vault data.');
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
        void navigator.locks.request('vault-open-' + account!.account, { ifAvailable: true }, async (held) => { if (!held) {
            reject(Error('Vault open elsewhere'));
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
        localStorage.setItem('vault-account', JSON.stringify(account));
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
    card.append(node('span', 'ENCRYPTED DOCUMENT STORAGE', 'eyebrow'), node('h1', account ? 'Welcome back.' : 'A place for what matters.'), node('p', account ? 'Unlock your local vault. Synced documents are available offline.' : 'Create a vault, or open the account you already use on desktop.', 'muted'));
    const form = node('form'), password = field('Password', 'password');
    password.input.required = true;
    password.input.autocomplete = account ? 'current-password' : 'new-password';
    form.append(password.wrap);
    if (account) {
        const go = node('button', 'Unlock vault', 'primary');
        go.type = 'submit';
        form.append(go);
        form.onsubmit = async (e) => { e.preventDefault(); if (busy)
            return; busy = true; go.disabled = true; const value = password.input.value; password.input.value = ''; message('Unlocking…'); try {
            await enter(value);
        }
        catch {
            message('Could not unlock. Check the password and close other tabs using this vault.');
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
        const go = node('button', 'Create vault', 'primary');
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
                message('Could not create the vault. Browser storage must be available.');
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
        imp.input.accept = '.vault,.v1,application/octet-stream';
        imp.input.onchange = async () => { try {
            const file = imp.input.files?.[0];
            if (!file)
                return;
            c.check(file.size === 260);
            account = c.parseAccount(new Uint8Array(await file.arrayBuffer()));
            locked();
        }
        catch {
            message('This is not a valid Vault account file.');
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
function recoveryDialog(code: string) { const dialog = node('dialog'); dialog.append(node('h2', 'Save your recovery code'), node('p', 'Store this separately from your vault. It is shown once and is needed if you lose your password.'), node('code', code)); dialog.append(button('Download recovery code', () => download(c.utf8(code + '\n'), 'vault-recovery.txt', 'text/plain')), button('I have saved it', () => { dialog.close(); dialog.remove(); })); app.append(dialog); dialog.showModal(); dialog.oncancel = e => e.preventDefault(); }
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
function trashed(h: Head, seen = new Set<string>()): boolean { if (h.metadata.tags.includes('vault:system:trash') && h.metadata.type === 'folder')
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
    const search = field('Search names and tags');
    search.input.value = filter;
    search.input.oninput = () => { filter = search.input.value; renderRows(); };
    main.append(search.wrap);
    const list = node('div', undefined, 'documents');
    main.append(list);
    function renderRows() {
        list.replaceChildren();
        const visible = heads.filter(h => !trashed(h) && (!favorites || h.metadata.favorite) && (favorites || h.metadata.parent === parent)&&(!filter || (h.metadata.name + ' ' + h.metadata.tags.join(' ')).toLocaleLowerCase().includes(filter.toLocaleLowerCase())));
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
 row.append(button('Edit', () => editDialog(h)));
            list.append(row);
        }
    }
    renderRows();
    const status = node('p', pending ? `${pending} local change${pending === 1 ? '' : 's'} waiting to sync.` : 'Local vault ready.', 'status');
    status.id = 'status';
    status.setAttribute('role', 'status');
    main.append(status);
    const tools = node('details');
    tools.append(node('summary', 'Vault settings'), node('p', 'Account ID: ' + account!.account, 'account-id'), button('Download encrypted account file', () => download(c.accountBytes(account!), 'account.vault')), button('Register this vault on server', () => void syncNow(true)), button('New folder', () => folderDialog()), node('p', 'Keep the account file and recovery code safe. Browser storage can be cleared or evicted.', 'footnote'));
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
        if (!n.trim() || n.length > 255 || /[\x00/\\]/.test(n) || t.some(x => x.startsWith('vault:system:')))
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
    const saved = localStorage.getItem('vault-account');
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
