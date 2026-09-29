import sodium from 'libsodium-wrappers-sumo';
export const ready = sodium.ready;
export const CHUNK = 4194304, MAX_METADATA = 1048576, ZERO = '0'.repeat(32);
export function check(ok: unknown): asserts ok { if (!ok)
    throw Error('Invalid vault data'); }
export const wipe = (b: Uint8Array) => sodium.memzero(b);
export const random = (n: number) => sodium.randombytes_buf(n);
export const hex = (b: Uint8Array) => Array.from(b, x => x.toString(16).padStart(2, '0')).join('');
export function unhex(s: string) { check(typeof s === 'string' && /^(?:[0-9a-f]{2})*$/.test(s)); return Uint8Array.from(s.match(/../g) || [], x => parseInt(x, 16)); }
export function validID(s: unknown): asserts s is string { check(typeof s === 'string' && /^[0-9a-f]{32}$/.test(s) && s !== ZERO); }
export function id() { let s: string; do {
    s = hex(random(16));
} while (s === ZERO); return s; }
export function cat(...xs: Uint8Array[]) { const b = new Uint8Array(xs.reduce((n, x) => n + x.length, 0)); let n = 0; for (const x of xs) {
    b.set(x, n);
    n += x.length;
} return b; }
export function utf8(s: string) {
    check(typeof s === 'string');
    for (let i = 0; i < s.length; i++) {
        const n = s.charCodeAt(i);
        if (n >= 0xd800 && n <= 0xdbff) {
            const low = s.charCodeAt(++i);
            check(low >= 0xdc00 && low <= 0xdfff);
        }
        else
            check(n < 0xdc00 || n > 0xdfff);
    }
    return new TextEncoder().encode(s);
}
export function decode(b: Uint8Array) { return new TextDecoder('utf-8', { fatal: true, ignoreBOM: true }).decode(b); }
export function base64(b: Uint8Array) { let s = ''; for (let i = 0; i < b.length; i += 8192)
    s += String.fromCharCode(...b.subarray(i, i + 8192)); return btoa(s); }
export function unbase64(s: string) { check(typeof s === 'string'); const b = Uint8Array.from(atob(s), x => x.charCodeAt(0)); check(base64(b) === s); return b; }
const view = (b: Uint8Array) => new DataView(b.buffer, b.byteOffset, b.byteLength);
export function descriptor(d: Uint8Array) { check(d.length === 27 && d[0] === 1 && d[1] === 1 && view(d).getUint32(2) === 65536 && view(d).getUint32(6) === 3 && d[10] === 1); }
export function derive(password: string, d: Uint8Array) {
    descriptor(d);
    const p = utf8(password);
    try {
        check(p.length <= 1024);
        return sodium.crypto_pwhash(64, p, d.subarray(11), 3, 67108864, sodium.crypto_pwhash_ALG_ARGON2ID13);
    }
    finally {
        wipe(p);
    }
}
export function credential(auth: Uint8Array, account: string) { validID(account); check(auth.length === 32); return sodium.crypto_auth_hmacsha256(cat(utf8('vault/auth/v1\0'), unhex(account)), auth); }
export interface Context {
    kind: number;
    account: string;
    object: string;
    index?: number;
    final?: boolean;
}
function context(c: Context) {
    validID(c.account);
    check(Number.isInteger(c.kind) && c.kind >= 1 && c.kind <= 5);
    if (c.kind <= 2)
        check(c.object === ZERO);
    else
        validID(c.object);
    if (c.kind === 5)
        check(Number.isSafeInteger(c.index) && c.index! >= 0 && c.index! < chunkCount(Number.MAX_SAFE_INTEGER) && typeof c.final === 'boolean');
}
function shape(c: Context, n: number) {
    if (c.kind <= 3)
        check(n === 32);
    else if (c.kind === 4)
        check(n <= MAX_METADATA);
    else
        check(n <= CHUNK && (c.final || n === CHUNK) && (n !== 0 || (c.index === 0 && c.final)));
}
function extraAAD(c: Context, extra: Uint8Array) { if (c.kind === 1)
    descriptor(extra);
else
    check(extra.length === 0); }
export function seal(key: Uint8Array, c: Context, p: Uint8Array, extra = new Uint8Array(), nonce = random(24)) {
    context(c);
    check(key.length === 32 && nonce.length === 24);
    shape(c, p.length);
    extraAAD(c, extra);
    let h = cat(new Uint8Array([1, c.kind]), unhex(c.account), unhex(c.object));
    if (c.kind === 5) {
        const f = new Uint8Array(13);
        view(f).setBigUint64(0, BigInt(c.index!));
        f[8] = c.final ? 1 : 0;
        view(f).setUint32(9, p.length);
        h = cat(h, f);
    }
    h = cat(h, nonce);
    return cat(h, sodium.crypto_aead_xchacha20poly1305_ietf_encrypt(p, cat(utf8('vault/record/v1\0'), h, extra), null, nonce, key));
}
export function open(key: Uint8Array, c: Context, b: Uint8Array, extra = new Uint8Array()) {
    context(c);
    check(key.length === 32);
    extraAAD(c, extra);
    const n = c.kind === 5 ? 71 : 58;
    check(b.length >= n + 16 && b[0] === 1 && b[1] === c.kind && hex(b.subarray(2, 18)) === c.account && hex(b.subarray(18, 34)) === c.object);
    shape(c, b.length - n - 16);
    if (c.kind === 5)
        check(view(b).getBigUint64(34) === BigInt(c.index!) && b[42] === (c.final ? 1 : 0) && view(b).getUint32(43) === b.length - 87);
    try {
        return sodium.crypto_aead_xchacha20poly1305_ietf_decrypt(null, b.subarray(n), cat(utf8('vault/record/v1\0'), b.subarray(0, n), extra), b.subarray(n - 24, n), key);
    }
    catch {
        throw Error('Vault authentication failed');
    }
}
export function chunkCount(n: number) { check(Number.isSafeInteger(n) && n >= 0); return Math.max(1, Math.ceil(n / CHUNK)); }
export function decryptFile(key: Uint8Array, account: string, file: string, size: number, chunks: Uint8Array[]) {
    check(chunks.length === chunkCount(size));
    const parts: Uint8Array[] = [];
    try {
        let total = 0;
        for (let i = 0; i < chunks.length; i++) {
            const p = open(key, { kind: 5, account, object: file, index: i, final: i === chunks.length - 1 }, chunks[i]);
            parts.push(p);
            total += p.length;
        }
        check(total === size);
        return cat(...parts);
    }
    finally {
        parts.forEach(wipe);
    }
}
// Scan JSON before JSON.parse, which otherwise silently accepts duplicate keys.
export function strictJSON(text: string): unknown {
    let i = 0;
    const ws = () => { while (/[\x20\t\r\n]/.test(text[i] || '!'))
        i++; };
    const string = (): string => { const start = i++; for (; i < text.length; i++) {
        if (text[i] === '\\') {
            i++;
            continue;
        }
        if (text[i] === '"') {
            const s = JSON.parse(text.slice(start, ++i));
            utf8(s);
            return s;
        }
    } throw Error('Invalid vault data'); };
    function value(depth: number): void {
        check(depth <= 8);
        ws();
        const ch = text[i];
        if (ch === '"') {
            string();
            return;
        }
        if (ch === '{' || ch === '[') {
            i++;
            ws();
            const end = ch === '{' ? '}' : ']', seen = new Set<string>();
            if (text[i] === end) {
                i++;
                return;
            }
            for (;;) {
                ws();
                if (ch === '{') {
                    check(text[i] === '"');
                    const k = string();
                    check(!seen.has(k));
                    seen.add(k);
                    ws();
                    check(text[i++] === ':');
                }
                value(depth + 1);
                ws();
                if (text[i] === end) {
                    i++;
                    return;
                }
                check(text[i++] === ',');
            }
        }
        const m = /^(?:true|false|null|-?(?:0|[1-9]\d*)(?:\.\d+)?(?:[eE][+-]?\d+)?)/.exec(text.slice(i));
        check(m);
        i += m[0].length;
    }
    value(0);
    ws();
    check(i === text.length);
    return JSON.parse(text);
}
export interface Metadata {
    type: 'file' | 'folder';
    name: string;
    parent: string | null;
    tags: string[];
    favorite: boolean;
    file_id?: string;
    size?: number;
    chunk_count?: number;
}
export function metadata(b: Uint8Array): Metadata {
    check(b.length <= MAX_METADATA);
    const m = strictJSON(decode(b)) as Metadata;
    check(m && typeof m === 'object' && !Array.isArray(m) && (m.type === 'file' || m.type === 'folder') && typeof m.name === 'string' && Array.isArray(m.tags) && m.tags.every(t => typeof t === 'string') && typeof m.favorite === 'boolean');
    if (m.parent !== null)
        validID(m.parent);
    if (m.type === 'file') {
        validID(m.file_id);
        check(Number.isSafeInteger(m.size) && m.size! >= 0 && m.chunk_count === chunkCount(m.size!));
    }
    else
        for (const k of ['file_id', 'size', 'chunk_count'])
            check(!Object.hasOwn(m, k));
    return m;
}
export function encryptMetadata(key: Uint8Array, account: string, object: string, m: Metadata) { const p = utf8(JSON.stringify(m)); try {
    metadata(p);
    return seal(key, { kind: 4, account, object }, p);
}
finally {
    wipe(p);
} }
export function decryptMetadata(key: Uint8Array, account: string, object: string, b: Uint8Array) { const p = open(key, { kind: 4, account, object }, b); try {
    return metadata(p);
}
finally {
    wipe(p);
} }
const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567';
export function recoveryEncode(b: Uint8Array) { check(b.length === 32); let bits = 0, acc = 0, s = 'vlt1-'; for (const x of b) {
    acc = (acc << 8) | x;
    bits += 8;
    while (bits >= 5) {
        bits -= 5;
        s += alphabet[(acc >>> bits) & 31];
    }
} if (bits)
    s += alphabet[(acc << (5 - bits)) & 31]; return s; }
export function recoveryDecode(s: string) { check(/^vlt1-[A-Z2-7]{52}$/.test(s)); let bits = 0, acc = 0; const out: number[] = []; for (const ch of s.slice(5)) {
    acc = (acc << 5) | alphabet.indexOf(ch);
    bits += 5;
    if (bits >= 8) {
        bits -= 8;
        out.push((acc >>> bits) & 255);
    }
} const b = new Uint8Array(out); check(recoveryEncode(b) === s); return b; }
export interface Account {
    account: string;
    descriptor: string;
    password_wrapper: string;
    recovery_wrapper: string;
}
export function validateAccount(a: Account) {
    validID(a.account);
    descriptor(unbase64(a.descriptor));
    for (const [k, s] of [[1, a.password_wrapper], [2, a.recovery_wrapper]] as const) {
        const b = unbase64(s);
        check(b.length === 106 && b[0] === 1 && b[1] === k && hex(b.subarray(2, 18)) === a.account && hex(b.subarray(18, 34)) === ZERO);
    }
    return a;
}
export function parseAccount(b: Uint8Array): Account { check(b.length === 260 && hex(b.subarray(0, 5)) === '564c544101'); return validateAccount({ account: hex(b.subarray(5, 21)), descriptor: base64(b.subarray(21, 48)), password_wrapper: base64(b.subarray(48, 154)), recovery_wrapper: base64(b.subarray(154)) }); }
export function accountBytes(a: Account) { validateAccount(a); return cat(unhex('564c544101'), unhex(a.account), unbase64(a.descriptor), unbase64(a.password_wrapper), unbase64(a.recovery_wrapper)); }
export function unlock(a: Account, password: string) { validateAccount(a); const d = unbase64(a.descriptor), k = derive(password, d); try {
    return { master: open(k.subarray(0, 32), { kind: 1, account: a.account, object: ZERO }, unbase64(a.password_wrapper), d), credential: credential(k.subarray(32), a.account) };
}
finally {
    wipe(k);
} }
export function createAccount(password: string) {
    check(utf8(password).length > 0);
    const account = id(), d = cat(unhex('0101000100000000000301'), random(16)), k = derive(password, d), master = random(32), recovery = random(32);
    try {
        return { account: { account, descriptor: base64(d), password_wrapper: base64(seal(k.subarray(0, 32), { kind: 1, account, object: ZERO }, master, d)), recovery_wrapper: base64(seal(recovery, { kind: 2, account, object: ZERO }, master)) }, master, recovery: recoveryEncode(recovery) };
    }
    finally {
        wipe(k);
        wipe(recovery);
    }
}
