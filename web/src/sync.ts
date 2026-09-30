import * as c from './crypto.ts';
import { Store, unlockRevision, type Revision } from './store.ts';
export class HTTPError extends Error {
    constructor(readonly status: number, readonly after: number) { super('Vault server returned ' + status); }
}
export interface Session {
    token: string;
    device: string;
    expires: number;
}
export class Remote {
    token = '';
    device = '';
    constructor(readonly origin: string, readonly signal: AbortSignal) {
        const u = new URL(origin);
        c.check(u.origin === origin && !u.username && !u.password && (u.protocol === 'https:' || (u.protocol === 'http:' && ['127.0.0.1', '[::1]', 'localhost'].includes(u.hostname))));
    }
    async request(method: string, path: string, body?: unknown, limit = 24 * 1024 * 1024): Promise<Uint8Array> {
        for (let attempt = 0;; attempt++) {
            this.signal.throwIfAborted();
            let delay = 250 * 2 ** attempt;
            try {
                const controller = new AbortController(), timer = setTimeout(() => controller.abort(), 30000), abort = () => controller.abort();
                this.signal.addEventListener('abort', abort, { once: true });
                try {
                    const binary = body instanceof Uint8Array;
                    const res = await fetch(this.origin + path, { method, body: body === undefined ? undefined : binary ? new Blob([body as Uint8Array<ArrayBuffer>]) : JSON.stringify(body), headers: { ...(this.token ? { Authorization: 'Bearer ' + this.token } : {}), ...(body === undefined ? {} : { 'Content-Type': binary ? 'application/octet-stream' : 'application/json' }) }, redirect: 'error', credentials: 'omit', cache: 'no-store', signal: controller.signal });
                    if (!res.ok) {
                        await res.body?.cancel();
                        const seconds = Number(res.headers.get('Retry-After'));
                        throw new HTTPError(res.status, Number.isInteger(seconds) && seconds > 0 && seconds <= 120 ? seconds * 1000 : 0);
                    }
                    const reader = res.body?.getReader();
                    if (!reader)
                        return new Uint8Array();
                    const parts: Uint8Array[] = [];
                    let size = 0;
                    try {
                        for (;;) {
                            const { done, value } = await reader.read();
                            if (done)
                                break;
                            size += value.length;
                            c.check(size <= limit);
                            parts.push(value);
                        }
                        return c.cat(...parts);
                    }
                    finally {
                        await reader.cancel();
                    }
                }
                finally {
                    clearTimeout(timer);
                    this.signal.removeEventListener('abort', abort);
                }
            }
            catch (e) {
                this.signal.throwIfAborted();
                const retry = e instanceof TypeError || (e instanceof DOMException && e.name === 'AbortError') || (e instanceof HTTPError && (e.status === 408 || e.status === 429 || e.status >= 500));
                if (!retry || attempt === 3)
                    throw e;
                if (e instanceof HTTPError)
                    delay = Math.max(delay, e.after);
                await new Promise<void>((resolve, reject) => { const abort = () => { clearTimeout(timer); reject(Error('Locked')); }; const timer = setTimeout(() => { this.signal.removeEventListener('abort', abort); resolve(); }, delay); this.signal.addEventListener('abort', abort, { once: true }); });
            }
        }
    }
    async json<T>(method: string, path: string, body?: unknown, limit?: number): Promise<T> { return c.strictJSON(c.decode(await this.request(method, path, body, limit))) as T; }
    async login(a: c.Account, credential: Uint8Array, signup = false) {
        const s = await this.json<Session>('POST', signup ? '/v1/signup' : '/v1/login', signup ? { ...a, credential: c.base64(credential) } : { account: a.account, credential: c.base64(credential) }, 8192);
        c.check(/^[0-9a-f]{64}$/.test(s.token) && Number.isSafeInteger(s.expires) && s.expires > 0);
        c.validID(s.device);
        this.token = s.token;
        this.device = s.device;
    }
}
export async function sync(store: Store, master: Uint8Array, remote: Remote) {
    await store.bind(remote.origin);
    for (const { revision: r } of (await store.all()).filter(x => x.pending).sort((a, b) => a.order - b.order)) {
        remote.signal.throwIfAborted();
        const { metadata: m, key } = unlockRevision(master, store.account.account, r);
        if (key)
            try {
                const chunks = await store.chunks(m.file_id!, m.chunk_count!), plain = c.decryptFile(key, store.account.account, m.file_id!, m.size!, chunks);
                c.wipe(plain);
                const present = new Set<number>();
                let after = -1;
                for (;;) {
                    const page = await remote.json<number[]>('GET', `/v1/blobs/${m.file_id}/chunks${after >= 0 ? '?after=' + after : ''}`, undefined, 32768);
                    c.check(Array.isArray(page) && page.length <= 1000);
                    if (!page.length)
                        break;
                    for (const index of page) {
                        c.check(Number.isSafeInteger(index) && index > after && index < m.chunk_count!);
                        present.add(index);
                        after = index;
                    }
                }
                for (let i = 0; i < chunks.length; i++)
                    if (!present.has(i))
                        await remote.request('PUT', `/v1/blobs/${m.file_id}/chunks/${i}`, chunks[i], 1024);
            }
            finally {
                c.wipe(key);
            }
        const result = await remote.json<{
            sequence: number;
        }>('PUT', '/v1/sync/revisions/' + r.id, r, 1024);
        c.check(Number.isSafeInteger(result.sequence) && result.sequence > 0);
        await store.acknowledge(r.id);
    }
    let cursor = await store.state<number>('cursor') || 0;
    for (;;) {
        const page = await remote.json<{
            changes: {
                sequence: number;
                revision: Revision;
            }[];
        }>('GET', '/v1/sync/changes?after=' + cursor);
        c.check(Array.isArray(page.changes) && page.changes.length <= 16);
        if (!page.changes.length)
            break;
        for (const change of page.changes) {
            c.check(Number.isSafeInteger(change.sequence) && change.sequence > cursor);
            const r = change.revision, { metadata: m, key } = unlockRevision(master, store.account.account, r), chunks: Uint8Array[] = [];
            if (key)
                try {
                    // Validate each bounded chunk before accumulating ciphertext. No partial import is visible.
                    let total = 0;
                    for (let i = 0; i < m.chunk_count!; i++) {
                        const b = await remote.request('GET', `/v1/blobs/${m.file_id}/chunks/${i}`, undefined, c.CHUNK + 87);
                        const p = c.open(key, { kind: 5, account: store.account.account, object: m.file_id!, index: i, final: i === m.chunk_count! - 1 }, b);
                        total += p.length;
                        c.wipe(p);
                        chunks.push(b);
                    }
                    c.check(total === m.size);
                }
                finally {
                    c.wipe(key);
                }
            remote.signal.throwIfAborted();
            await store.commit(r, chunks, m.file_id, false, change.sequence);
            cursor = change.sequence;
        }
    }
    await remote.request('PUT', '/v1/sync/cursor', { sequence: cursor }, 1024);
}
