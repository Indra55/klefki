// M0 public fixture tooling, never production code. Node 22.18+ type stripping.
import { readFileSync } from 'node:fs';
import { createHash, createHmac } from 'node:crypto';
import sodium from 'libsodium-wrappers-sumo';

type RecordVector = {
 id: string; kind: number; account_hex: string; object_hex: string;
 key_hex: string; nonce_hex: string; extra_aad_hex: string; plaintext_hex: string;
 index: number; final: boolean; record_hex: string;
};
const CHUNK = 4 * 1024 * 1024;
const hex = (b: Uint8Array) => Buffer.from(b).toString('hex');
const unhex = (s: string) => { if (!/^(?:[0-9a-f]{2})*$/.test(s)) throw Error('invalid fixture hex'); return Buffer.from(s, 'hex'); };
const seq = (n: number, start: number) => Buffer.from(Array.from({ length: n }, (_, i) => (start + i) & 255));
const hash = (b: Uint8Array) => createHash('sha256').update(b).digest();
const cat = (...xs: Uint8Array[]) => Buffer.concat(xs);
const text = (s: string) => Buffer.from(s, 'utf8');
const domain = text('vault/record/v1\0');
const account = seq(16, 0), object = seq(16, 16), fileKey = seq(32, 96);
function check(ok: boolean, id: string) { if (!ok) throw Error(`FAIL ${id}`); }
function derive(password: Buffer, d: Buffer) {
 check(d.length === 27 && d[0] === 1 && d[1] === 1 && d.readUInt32BE(2) === 65536 && d.readUInt32BE(6) === 3 && d[10] === 1 && password.length <= 1024, 'kdf-parameters');
 return Buffer.from(sodium.crypto_pwhash(64, password, d.subarray(11), 3, 65536 * 1024, sodium.crypto_pwhash_ALG_ARGON2ID13));
}
function seal(r: RecordVector) {
 const p = unhex(r.plaintext_hex), n = unhex(r.nonce_hex);
 let h = cat(Buffer.from([1, r.kind]), unhex(r.account_hex), unhex(r.object_hex));
 if (r.kind === 5) { const fields = Buffer.alloc(13); fields.writeBigUInt64BE(BigInt(r.index), 0); fields[8] = r.final ? 1 : 0; fields.writeUInt32BE(p.length, 9); h = cat(h, fields); }
 h = cat(h, n);
 return cat(h, sodium.crypto_aead_xchacha20poly1305_ietf_encrypt(p, cat(domain, h, unhex(r.extra_aad_hex)), null, n, unhex(r.key_hex)));
}
function open(b: Buffer, r: RecordVector) {
 const hlen = r.kind === 5 ? 71 : 58;
 check(b.length >= hlen + 16 && b[0] === 1 && b[1] === r.kind && b.subarray(2, 18).equals(unhex(r.account_hex)) && b.subarray(18, 34).equals(unhex(r.object_hex)), 'record-header');
 const plen = b.length - hlen - 16;
 if (r.kind === 5) {
  check(b[42] <= 1 && b.readBigUInt64BE(34) === BigInt(r.index) && (b[42] === 1) === r.final && b.readUInt32BE(43) === plen && plen <= CHUNK && (r.final || plen === CHUNK) && (plen !== 0 || (r.index === 0 && r.final)), 'chunk-shape');
 } else if (r.kind >= 1 && r.kind <= 3) check(plen === 32, 'wrapped-key-length');
 else check(r.kind === 4 && plen <= 1024 * 1024, 'metadata-length');
 return Buffer.from(sodium.crypto_aead_xchacha20poly1305_ietf_decrypt(null, b.subarray(hlen), cat(domain, b.subarray(0, hlen), unhex(r.extra_aad_hex)), b.subarray(hlen - 24, hlen), unhex(r.key_hex)));
}
function record(id: string, key: Buffer, p: Buffer, index: number, final: boolean): RecordVector {
 return { id, kind: 5, account_hex: hex(account), object_hex: hex(object), key_hex: hex(key), nonce_hex: hex(hash(text('vault-fixture/' + id)).subarray(0, 24)), extra_aad_hex: '', plaintext_hex: hex(p), index, final, record_hex: '' };
}
function stream(length: number) {
 const count = Math.max(1, Math.ceil(length / CHUNK)); const rs: RecordVector[] = [], bs: Buffer[] = [];
 for (let i = 0; i < count; i++) {
  const p = Buffer.alloc(Math.min(CHUNK, length - i * CHUNK)); for (let j = 0; j < p.length; j++) p[j] = (i * CHUNK + j) % 251;
  const r = record(`stream-${length}-${i}`, fileKey, p, i, i === count - 1); rs.push(r); bs.push(seal(r));
 }
 return { rs, bs };
}
function checkStream(bs: Buffer[], length: number) {
 check(bs.length === Math.max(1, Math.ceil(length / CHUNK)), 'stream-count'); let total = 0;
 bs.forEach((b, i) => { const p = open(b, record('unused', fileKey, Buffer.alloc(0), i, i === bs.length - 1)); total += p.length; for (let j = 0; j < p.length; j++) check(p[j] === (i * CHUNK + j) % 251, 'stream-content'); });
 check(total === length, 'stream-length');
}
function recoveryEncode(b: Buffer) {
 const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567'; let bits = 0, acc = 0, out = 'vlt1-';
 for (const x of b) { acc = (acc << 8) | x; bits += 8; while (bits >= 5) { bits -= 5; out += alphabet[(acc >>> bits) & 31]; } }
 if (bits) out += alphabet[(acc << (5 - bits)) & 31]; return out;
}
function recoveryDecode(s: string) {
 check(/^vlt1-[A-Z2-7]{52}$/.test(s), 'recovery-code'); const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567'; let bits = 0, acc = 0; const out: number[] = [];
 for (const c of s.slice(5)) { acc = (acc << 5) | alphabet.indexOf(c); bits += 5; if (bits >= 8) { bits -= 8; out.push((acc >>> bits) & 255); } }
 const b = Buffer.from(out); check(b.length === 32 && recoveryEncode(b) === s, 'recovery-canonical'); return b;
}
try {
 await sodium.ready;
 const v = JSON.parse(readFileSync(new URL('../../vectors-v1.json', import.meta.url), 'utf8'));
 check(v.fixture_version === 1 && v.kdf.length === 3 && v.records.length === 7 && v.streams.length === 6 && v.negative.length === 25, 'fixture-schema');
 for (const k of v.kdf) {
  const out = derive(unhex(k.password_utf8_hex), unhex(k.descriptor_hex));
  check(hex(out) === k.output_hex && hex(out.subarray(0, 32)) === k.kek_hex && hex(out.subarray(32)) === k.auth_key_hex, k.id);
  const credential = createHmac('sha256', out.subarray(32)).update(cat(text('vault/auth/v1\0'), account)).digest();
  check(hex(credential) === k.credential_hex && hex(hash(cat(text('vault/auth-verifier/v1\0'), credential))) === k.verifier_hex, k.id);
 }
 for (const r of v.records as RecordVector[]) { check(hex(seal(r)) === r.record_hex && hex(open(unhex(r.record_hex), r)) === r.plaintext_hex, r.id); }
 // Assert the key hierarchy, rather than merely independent envelope round trips.
 const byID = (id: string): RecordVector => v.records.find((r: RecordVector) => r.id === id);
 const pw = byID('master-password'), recovery = byID('master-recovery'), file = byID('file-key');
 check(pw.key_hex === v.kdf[0].kek_hex && pw.extra_aad_hex === v.kdf[0].descriptor_hex && recovery.key_hex === v.recovery_key_hex && pw.plaintext_hex === recovery.plaintext_hex && file.key_hex === pw.plaintext_hex && byID('chunk-binary').key_hex === file.plaintext_hex, 'key-hierarchy');
 check(hex(recoveryDecode(v.recovery_code)) === v.recovery_key_hex && recoveryEncode(unhex(v.recovery_key_hex)) === v.recovery_code, 'recovery');
 const recoveredMaster = open(unhex(recovery.record_hex), { ...recovery, key_hex: hex(recoveryDecode(v.recovery_code)) });
 const recoveredFileKey = open(unhex(file.record_hex), { ...file, key_hex: hex(recoveredMaster) });
 const document = byID('chunk-binary');
 check(hex(open(unhex(document.record_hex), { ...document, key_hex: hex(recoveredFileKey) })) === document.plaintext_hex, 'recovered-document');
 for (const s of ['', 'vlt1-', 'VLT1-AAAA', v.recovery_code + 'A']) { let rejected = false; try { recoveryDecode(s); } catch { rejected = true; } check(rejected, 'recovery-invalid'); }
 for (const s of v.streams) { const { bs } = stream(s.length); check(JSON.stringify(bs.map(b => hex(hash(b)))) === JSON.stringify(s.record_sha256), s.id); checkStream(bs, s.length); }
 for (const n of v.negative) {
  let rejected = false;
  // Unknown operations fail outside the expected-rejection handler.
  const ops = ['flip-ciphertext','flip-nonce','wrong-key','wrong-account','wrong-object','wrong-kind','unknown-version','short-record','trailing-byte','wrong-index','wrong-final','wrong-length','invalid-final','wrong-extra','reorder','drop-first','drop-final','truncate-final','append-record','mix-revision','kdf-memory','kdf-time','kdf-lanes','kdf-version','kdf-short'];
  check(ops.includes(n.operation), n.id);
  try {
   if (n.base === 'kdf-0') {
    let d = unhex(v.kdf[0].descriptor_hex);
    switch (n.operation) { case 'kdf-memory': d[2] = 255; break; case 'kdf-time': d[9] = 0; break; case 'kdf-lanes': d[10] = 2; break; case 'kdf-version': d[0] = 2; break; case 'kdf-short': d = d.subarray(0, 26); break; }
    derive(unhex(v.kdf[0].password_utf8_hex), d);
   } else if (n.base.startsWith('stream-')) {
    let { rs, bs } = stream(CHUNK + 1);
    switch (n.operation) { case 'reorder': [bs[0], bs[1]] = [bs[1], bs[0]]; break; case 'drop-first': bs = bs.slice(1); break; case 'drop-final': bs = bs.slice(0, 1); break; case 'truncate-final': bs[1] = bs[1].subarray(0, bs[1].length - 1); break; case 'append-record': bs.push(bs[1]); break; case 'mix-revision': rs[1].key_hex = hex(seq(32, 128)); bs[1] = seal(rs[1]); break; }
    checkStream(bs, CHUNK + 1);
   } else {
    const r = { ...byID(n.base) }; let b = unhex(r.record_hex);
    switch (n.operation) { case 'flip-ciphertext': b[b.length - 1] ^= 1; break; case 'flip-nonce': b[47] ^= 1; break; case 'wrong-key': r.key_hex = hex(seq(32, 200)); break; case 'wrong-account': r.account_hex = hex(seq(16, 200)); break; case 'wrong-object': r.object_hex = hex(seq(16, 200)); break; case 'wrong-kind': r.kind = 4; break; case 'unknown-version': b[0] = 2; break; case 'short-record': b = b.subarray(0, 12); break; case 'trailing-byte': b = cat(b, Buffer.from([0])); break; case 'wrong-index': b[41] = 1; break; case 'wrong-final': b[42] = 0; break; case 'wrong-length': b[46] ^= 1; break; case 'invalid-final': b[42] = 2; break; case 'wrong-extra': { const d = unhex(r.extra_aad_hex); d[26] ^= 1; r.extra_aad_hex = hex(d); break; } }
    open(b, r);
   }
  } catch { rejected = true; }
  check(rejected, n.id);
 }
 console.log('PASS TypeScript vectors');
} catch { console.error('FAIL TypeScript vectors'); process.exitCode = 1; }
