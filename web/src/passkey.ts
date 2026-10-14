import sodium from 'libsodium-wrappers-sumo';
import * as c from './crypto.ts';

const prefix = 'klefki-passkey-v1-';
const domain = 'klefki/web-passkey/v1\0';
interface Record {
    version: 1;
    origin: string;
    rpId: string;
    accountBinding: string;
    credentialId: string;
    salt: string;
    nonce: string;
    wrapped: string;
}
export interface Keys { master: Uint8Array; credential: Uint8Array; }

function fail(code: string): never { throw Error(code); }
function require(ok: unknown): asserts ok { if (!ok) fail('PASSKEY_INVALID'); }
const buffer = (b: Uint8Array) => b as Uint8Array<ArrayBuffer>;
function bytes(value: BufferSource): Uint8Array<ArrayBuffer> {
    return value instanceof ArrayBuffer ? new Uint8Array(value) : new Uint8Array(value.buffer as ArrayBuffer, value.byteOffset, value.byteLength);
}
function base64url(b: Uint8Array) { return c.base64(b).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, ''); }
function credentialBytes(id: string) {
    require(typeof id === 'string' && /^[A-Za-z0-9_-]{1,1366}$/.test(id));
    const b = c.unbase64(id.replace(/-/g, '+').replace(/_/g, '/') + '='.repeat((4 - id.length % 4) % 4));
    require(b.length > 0 && b.length <= 1024 && base64url(b) === id);
    return b;
}
function encoded(value: string, length: number) {
    require(typeof value === 'string' && value.length <= Math.ceil(length / 3) * 4);
    const b = c.unbase64(value);
    require(b.length === length);
    return b;
}
async function binding(account: c.Account) {
    return c.base64(new Uint8Array(await crypto.subtle.digest('SHA-256', buffer(c.accountBytes(account)))));
}
function aad(record: Record) {
    return c.utf8(JSON.stringify([domain, record.version, record.origin, record.rpId, record.accountBinding, record.credentialId, record.salt]));
}
async function wrappingKey(secret: Uint8Array, record: Record) {
    const key = await crypto.subtle.importKey('raw', buffer(secret), 'HKDF', false, ['deriveBits']);
    return new Uint8Array(await crypto.subtle.deriveBits({ name: 'HKDF', hash: 'SHA-256', salt: buffer(encoded(record.accountBinding, 32)), info: buffer(aad(record)) }, key, 256));
}
export async function available() {
    if (!globalThis.isSecureContext || /^\d+\.\d+\.\d+\.\d+$/.test(location.hostname) || location.hostname.includes(':') || typeof PublicKeyCredential === 'undefined' || !navigator.credentials?.create || !navigator.credentials?.get)
        return false;
    return true;
}
export function configured(account: c.Account) {
    try { return localStorage.getItem(prefix + account.account) !== null; }
    catch { return false; }
}
export function remove(account: c.Account) { localStorage.removeItem(prefix + account.account); }
async function load(account: c.Account): Promise<{ record: Record; stored: string }> {
    const raw = localStorage.getItem(prefix + account.account);
    if (!raw) fail('PASSKEY_MISSING');
    require(raw.length <= 4096);
    const r = c.strictJSON(raw) as Record;
    require(r && r.version === 1 && r.origin === location.origin && r.rpId === location.hostname && r.accountBinding === await binding(account));
    credentialBytes(r.credentialId);
    encoded(r.salt, 32); encoded(r.nonce, 24); encoded(r.wrapped, 80);
    return { record: r, stored: raw };
}
async function validateResponse(result: PublicKeyCredential, type: 'webauthn.create' | 'webauthn.get', challenge: Uint8Array, rpId: string) {
    require(result.type === 'public-key' && result.rawId.byteLength > 0 && result.rawId.byteLength <= 1024);
    require(result.response.clientDataJSON.byteLength <= 4096);
    const data = c.strictJSON(c.decode(new Uint8Array(result.response.clientDataJSON))) as { type?: string; challenge?: string; origin?: string; crossOrigin?: boolean };
    require(data.type === type && data.challenge === base64url(challenge) && data.origin === location.origin && data.crossOrigin !== true);
    const auth = type === 'webauthn.create'
        ? new Uint8Array((result.response as AuthenticatorAttestationResponse).getAuthenticatorData())
        : new Uint8Array((result.response as AuthenticatorAssertionResponse).authenticatorData);
    const hash = new Uint8Array(await crypto.subtle.digest('SHA-256', buffer(c.utf8(rpId))));
    require(auth.length >= 37 && c.hex(auth.subarray(0, 32)) === c.hex(hash) && (auth[32] & 5) === 5);
}
function clearOutput(output: AuthenticationExtensionsPRFValues | undefined) {
    if (output) { c.wipe(bytes(output.first)); if (output.second) c.wipe(bytes(output.second)); }
}
async function assertion(record: Record, signal: AbortSignal) {
    const challenge = c.random(32);
    const result = await navigator.credentials.get({ signal, publicKey: {
        challenge: buffer(challenge), rpId: record.rpId, timeout: 60000,
        allowCredentials: [{ type: 'public-key', id: buffer(credentialBytes(record.credentialId)) }],
        userVerification: 'required', extensions: { prf: { eval: { first: buffer(encoded(record.salt, 32)) } } },
    } }) as PublicKeyCredential | null;
    const output = result?.getClientExtensionResults().prf?.results;
    try {
        signal.throwIfAborted();
        if (!result) fail('PASSKEY_CANCELLED');
        require(base64url(new Uint8Array(result.rawId)) === record.credentialId);
        await validateResponse(result, 'webauthn.get', challenge, record.rpId);
        signal.throwIfAborted();
        if (!output) fail('PASSKEY_UNSUPPORTED');
        const secret = bytes(output.first);
        require(secret.length === 32);
        return secret.slice();
    }
    finally { clearOutput(output); }
}
function seal(key: Uint8Array, record: Record, keys: Keys) {
    require(keys.master.length === 32 && keys.credential.length === 32);
    const plain = c.cat(keys.master, keys.credential);
    try { return c.base64(sodium.crypto_aead_xchacha20poly1305_ietf_encrypt(plain, aad(record), null, encoded(record.nonce, 24), key)); }
    finally { c.wipe(plain); }
}
function open(key: Uint8Array, record: Record): Keys {
    const plain = sodium.crypto_aead_xchacha20poly1305_ietf_decrypt(null, encoded(record.wrapped, 80), aad(record), encoded(record.nonce, 24), key);
    try { require(plain.length === 64); return { master: plain.slice(0, 32), credential: plain.slice(32) }; }
    finally { c.wipe(plain); }
}
export async function enable(account: c.Account, keys: Keys, signal: AbortSignal) {
    const copies = { master: keys.master.slice(), credential: keys.credential.slice() };
    let secret: Uint8Array | undefined, key: Uint8Array | undefined, verified: Keys | undefined;
    try {
        if (!await available()) fail('PASSKEY_UNAVAILABLE');
        signal.throwIfAborted();
        const record: Record = { version: 1, origin: location.origin, rpId: location.hostname, accountBinding: await binding(account), credentialId: '', salt: c.base64(c.random(32)), nonce: c.base64(c.random(24)), wrapped: '' };
        signal.throwIfAborted();
        const challenge = c.random(32);
        const created = await navigator.credentials.create({ signal, publicKey: {
            challenge: buffer(challenge), rp: { id: record.rpId, name: 'Klefki' },
            user: { id: buffer(c.unhex(account.account)), name: account.account, displayName: 'Klefki document library' },
            pubKeyCredParams: [{ type: 'public-key', alg: -7 }, { type: 'public-key', alg: -8 }, { type: 'public-key', alg: -257 }],
            authenticatorSelection: { residentKey: 'required', userVerification: 'required' },
            attestation: 'none', timeout: 60000, extensions: { prf: { eval: { first: buffer(encoded(record.salt, 32)) } } },
        } }) as PublicKeyCredential | null;
        const extension = created?.getClientExtensionResults().prf;
        try {
            signal.throwIfAborted();
            if (!created) fail('PASSKEY_CANCELLED');
            await validateResponse(created, 'webauthn.create', challenge, record.rpId);
            if (!extension?.enabled) fail('PASSKEY_UNSUPPORTED');
            record.credentialId = base64url(new Uint8Array(created.rawId));
        }
        finally { clearOutput(extension?.results); }
        secret = await assertion(record, signal);
        key = await wrappingKey(secret, record);
        signal.throwIfAborted();
        record.wrapped = seal(key, record, copies);
        verified = open(key, record);
        require(sodium.memcmp(verified.master, copies.master) && sodium.memcmp(verified.credential, copies.credential));
        signal.throwIfAborted();
        localStorage.setItem(prefix + account.account, JSON.stringify(record));
    }
    finally {
        c.wipe(copies.master); c.wipe(copies.credential);
        if (secret) c.wipe(secret); if (key) c.wipe(key);
        if (verified) { c.wipe(verified.master); c.wipe(verified.credential); }
    }
}
export async function unlock(account: c.Account, signal: AbortSignal): Promise<Keys> {
    let secret: Uint8Array | undefined, key: Uint8Array | undefined;
    try {
        if (!await available()) fail('PASSKEY_UNAVAILABLE');
        const { record, stored } = await load(account);
        signal.throwIfAborted();
        secret = await assertion(record, signal);
        key = await wrappingKey(secret, record);
        signal.throwIfAborted();
        require(localStorage.getItem(prefix + account.account) === stored);
        return open(key, record);
    }
    finally { if (secret) c.wipe(secret); if (key) c.wipe(key); }
}
export function errorMessage(error: unknown) {
    const code = error instanceof Error ? error.message : '';
    const name = error instanceof Error ? error.name : '';
    if (name === 'NotAllowedError' || name === 'AbortError' || code === 'PASSKEY_CANCELLED')
        return 'Passkey verification was canceled or timed out. Try again, or use your password.';
    if (name === 'SecurityError' || code === 'PASSKEY_UNAVAILABLE' && (/^\d+\.\d+\.\d+\.\d+$/.test(location.hostname) || location.hostname.includes(':')))
        return 'Open Klefki on an HTTPS domain or localhost to use passkey unlock. IP-address URLs cannot use passkeys.';
    if (code === 'PASSKEY_UNAVAILABLE')
        return 'Passkey unlock needs HTTPS (or localhost) and a browser with WebAuthn. Use your password.';
    if (code === 'PASSKEY_UNSUPPORTED' || name === 'NotSupportedError')
        return 'This browser or passkey provider cannot protect your unlock keys with PRF. Use your password.';
    if (code === 'Klefki open elsewhere')
        return 'Close other tabs using this store, then try passkey or password unlock again.';
    return 'Passkey access could not be completed. Use your password; you can remove and set up passkey unlock again in settings.';
}
