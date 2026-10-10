import * as c from './crypto.ts';
export interface Revision {
    id: string;
    object: string;
    base: string;
    metadata: string;
    file_key?: string;
}
export interface Stored {
    revision: Revision;
    pending: boolean;
    order: number;
}
export interface Head {
    revision: Revision;
    metadata: c.Metadata;
    conflict: boolean;
}
export function validateRevision(r: Revision) { c.validID(r.id); c.validID(r.object); if (r.base !== '')
    c.validID(r.base); c.check(r.base !== r.id && typeof r.metadata === 'string' && (r.file_key === undefined || typeof r.file_key === 'string')); }
function sameRevision(a: Revision, b: Revision) { return a.id === b.id && a.object === b.object && a.base === b.base && a.metadata === b.metadata && (a.file_key || '') === (b.file_key || ''); }
export function unlockRevision(master: Uint8Array, account: string, r: Revision) {
    validateRevision(r);
    const metadata = c.decryptMetadata(master, account, r.object, c.unbase64(r.metadata));
    let key: Uint8Array | undefined;
    if (metadata.type === 'file') {
        c.check(r.file_key);
        key = c.open(master, { kind: 3, account, object: metadata.file_id! }, c.unbase64(r.file_key));
    }
    else
        c.check(!r.file_key);
    return { metadata, key };
}
const req = <T>(r: IDBRequest<T>) => new Promise<T>((resolve, reject) => { r.onsuccess = () => resolve(r.result); r.onerror = () => reject(Error('Local klefki storage failed')); });
const done = (tx: IDBTransaction) => new Promise<void>((resolve, reject) => { tx.oncomplete = () => resolve(); tx.onabort = tx.onerror = () => reject(Error('Local klefki transaction failed')); });
export class Store {
    private constructor(readonly db: IDBDatabase, readonly account: c.Account) { }
    static async open(account: c.Account) {
        c.validateAccount(account);
        const r = indexedDB.open('klefki-v1-' + account.account, 1);
        r.onupgradeneeded = () => { const db = r.result; db.createObjectStore('state'); db.createObjectStore('revisions', { keyPath: 'revision.id' }); db.createObjectStore('chunks'); };
        const db = await req(r);
        db.onversionchange = () => db.close();
        const store = new Store(db, account);
        await store.transaction(['state'], 'readwrite', async (tx) => { const state = tx.objectStore('state'), old = await req(state.get('account')); if (old)
            c.check(old.account === account.account && old.descriptor === account.descriptor && old.password_wrapper === account.password_wrapper && old.recovery_wrapper === account.recovery_wrapper);
        else
            state.put(account, 'account'); });
        return store;
    }
    close() { this.db.close(); }
    async transaction<T>(names: string[], mode: IDBTransactionMode, fn: (tx: IDBTransaction) => Promise<T>): Promise<T> {
        const tx = this.db.transaction(names, mode), completed = done(tx);
        try {
            const result = await fn(tx);
            await completed;
            return result;
        }
        catch (e) {
            try {
                tx.abort();
            }
            catch { }
            await completed.catch(() => { });
            throw e;
        }
    }
    async state<T>(key: string): Promise<T | undefined> { return this.transaction(['state'], 'readonly', tx => req(tx.objectStore('state').get(key))); }
    async bind(origin: string) { await this.transaction(['state'], 'readwrite', async (tx) => { const s = tx.objectStore('state'), old = await req(s.get('origin')); c.check(old === undefined || old === origin); s.put(origin, 'origin'); }); }
    async all(): Promise<Stored[]> { return this.transaction(['revisions'], 'readonly', tx => req(tx.objectStore('revisions').getAll())); }
    async revision(id: string): Promise<Revision> { c.validID(id); const x = await this.transaction(['revisions'], 'readonly', tx => req(tx.objectStore('revisions').get(id))); c.check(x); return x.revision; }
    async heads(master: Uint8Array): Promise<Head[]> { const all = await this.all(), parents = new Set(all.map(x => x.revision.base)), heads = all.filter(x => !parents.has(x.revision.id)); return heads.map(x => { const { metadata, key } = unlockRevision(master, this.account.account, x.revision); if (key)
        c.wipe(key); return { revision: x.revision, metadata, conflict: heads.filter(y => y.revision.object === x.revision.object).length > 1 }; }); }
    async chunks(file: string, count: number) { c.validID(file); return this.transaction(['chunks'], 'readonly', async (tx) => { const s = tx.objectStore('chunks'), out: Uint8Array[] = []; for (let i = 0; i < count; i++) {
        const b = await req(s.get(file + ':' + i));
        c.check(b instanceof Uint8Array);
        out.push(b);
    } return out; }); }
    async export(master: Uint8Array, r: Revision) { const { metadata: m, key } = unlockRevision(master, this.account.account, r); c.check(key); try {
        return c.decryptFile(key, this.account.account, m.file_id!, m.size!, await this.chunks(m.file_id!, m.chunk_count!));
    }
    finally {
        c.wipe(key);
    } }
    async commit(r: Revision, chunks: Uint8Array[], file: string | undefined, pending: boolean, sequence?: number) {
        validateRevision(r);
        await this.transaction(['revisions', 'chunks', 'state'], 'readwrite', async (tx) => {
            const revisions = tx.objectStore('revisions'), state = tx.objectStore('state'), blobs = tx.objectStore('chunks');
            const old: Stored | undefined = await req(revisions.get(r.id));
            if (old)
                c.check(sameRevision(old.revision, r));
            if (r.base) {
                const base: Stored | undefined = await req(revisions.get(r.base));
                c.check(base?.revision.object === r.object);
            }
            if (pending && r.base) {
                const all: Stored[] = await req(revisions.getAll());
                c.check(!all.some(x => x.revision.base === r.base));
            }
            for (let i = 0; i < chunks.length; i++) {
                c.check(file);
                const name = file + ':' + i, existing = await req(blobs.get(name));
                if (existing)
                    c.check(c.hex(existing) === c.hex(chunks[i]));
                else
                    blobs.add(chunks[i], name);
            }
            if (!old) {
                const order = (await req(state.get('order')) || 0) + 1;
                state.put(order, 'order');
                revisions.add({ revision: r, pending, order } satisfies Stored);
            }
            if (sequence !== undefined) {
                const cursor = await req(state.get('cursor')) || 0;
                c.check(Number.isSafeInteger(sequence) && sequence > cursor);
                state.put(sequence, 'cursor');
            }
        });
    }
    async acknowledge(id: string) { await this.transaction(['revisions'], 'readwrite', async (tx) => { const s = tx.objectStore('revisions'), x: Stored = await req(s.get(id)); c.check(x); x.pending = false; s.put(x); }); }
    async put(master: Uint8Array, m: c.Metadata, bytes?: Uint8Array, base?: Revision) {
        const object = base?.object || c.id(), r: Revision = { id: c.id(), object, base: base?.id || '', metadata: '' }, chunks: Uint8Array[] = [];
        let file: string | undefined;
        if (m.type === 'file') {
            c.check(bytes);
            file = c.id();
            m = { ...m, file_id: file, size: bytes.length, chunk_count: c.chunkCount(bytes.length) };
            const key = c.random(32);
            try {
                r.file_key = c.base64(c.seal(master, { kind: 3, account: this.account.account, object: file }, key));
                for (let i = 0; i < m.chunk_count!; i++)
                    chunks.push(c.seal(key, { kind: 5, account: this.account.account, object: file, index: i, final: i === m.chunk_count! - 1 }, bytes.subarray(i * c.CHUNK, (i + 1) * c.CHUNK)));
            }
            finally {
                c.wipe(key);
            }
        }
        r.metadata = c.base64(c.encryptMetadata(master, this.account.account, object, m));
        await this.commit(r, chunks, file, true);
        return r;
    }
    async edit(master: Uint8Array, base: Revision, edit: Pick<c.Metadata, 'name' | 'parent' | 'tags' | 'favorite'>) {
        const { metadata, key } = unlockRevision(master, this.account.account, base);
        if (key)
            c.wipe(key);
        const r: Revision = { ...base, id: c.id(), base: base.id, metadata: c.base64(c.encryptMetadata(master, this.account.account, base.object, { ...metadata, ...edit })) };
        await this.commit(r, [], undefined, true);
        return r;
    }
}
