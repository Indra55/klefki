const cases: {
    name: string;
    run: () => void;
}[] = [];
const test = (name: string, run: () => void) => cases.push({ name, run });
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createHash } from 'node:crypto';
import * as c from '../src/crypto.ts';
const v = JSON.parse(readFileSync(new URL('../../testvectors/vectors-v1.json', import.meta.url), 'utf8'));
await c.ready;
const hash = (b: Uint8Array) => createHash('sha256').update(b).digest('hex');
const context = (r: any) => ({ kind: r.kind, account: r.account_hex, object: r.object_hex, index: r.index, final: r.final });
const records = new Map<string, any>(v.records.map((r: any) => [r.id, r]));
for (const k of v.kdf)
    test(k.id, () => {
        const out = c.derive(c.decode(c.unhex(k.password_utf8_hex)), c.unhex(k.descriptor_hex));
        assert.equal(c.hex(out), k.output_hex);
        const credential = c.credential(out.subarray(32), v.records[0].account_hex);
        assert.equal(c.hex(credential), k.credential_hex);
        assert.equal(hash(c.cat(c.utf8('vault/auth-verifier/v1\0'), credential)), k.verifier_hex);
    });
for (const r of v.records)
    test(r.id, () => {
        const ctx = context(r), key = c.unhex(r.key_hex), extra = c.unhex(r.extra_aad_hex);
        assert.equal(c.hex(c.seal(key, ctx, c.unhex(r.plaintext_hex), extra, c.unhex(r.nonce_hex))), r.record_hex);
        assert.equal(c.hex(c.open(key, ctx, c.unhex(r.record_hex), extra)), r.plaintext_hex);
    });
function stream(length: number) {
    const key = c.unhex(records.get('chunk-binary').key_hex), chunks: Uint8Array[] = [];
    for (let i = 0; i < c.chunkCount(length); i++) {
        const p = Uint8Array.from({ length: Math.min(c.CHUNK, length - i * c.CHUNK) }, (_, j) => (i * c.CHUNK + j) % 251);
        const nonce = c.unhex(hash(c.utf8(`vault-fixture/stream-${length}-${i}`))).slice(0, 24);
        chunks.push(c.seal(key, { kind: 5, account: v.records[0].account_hex, object: '101112131415161718191a1b1c1d1e1f', index: i, final: i === c.chunkCount(length) - 1 }, p, new Uint8Array(), nonce));
    }
    return { key, chunks };
}
for (const s of v.streams)
    test(s.id, () => {
        const { key, chunks } = stream(s.length);
        assert.deepEqual(chunks.map(hash), s.record_sha256);
        assert.equal(c.decryptFile(key, v.records[0].account_hex, '101112131415161718191a1b1c1d1e1f', s.length, chunks).length, s.length);
    });
for (const n of v.negative)
    test(n.id, () => {
        assert.throws(() => {
            if (n.base === 'kdf-0') {
                let d = c.unhex(v.kdf[0].descriptor_hex);
                switch (n.operation) {
                    case 'kdf-memory':
                        d[2] = 255;
                        break;
                    case 'kdf-time':
                        d[9] = 0;
                        break;
                    case 'kdf-lanes':
                        d[10] = 2;
                        break;
                    case 'kdf-version':
                        d[0] = 2;
                        break;
                    case 'kdf-short':
                        d = d.slice(0, 26);
                        break;
                    default: assert.fail('unknown operation');
                }
                c.derive('', d);
                return;
            }
            if (n.base.startsWith('stream-')) {
                let { key, chunks } = stream(c.CHUNK + 1);
                switch (n.operation) {
                    case 'reorder':
                        chunks.reverse();
                        break;
                    case 'drop-first':
                        chunks = chunks.slice(1);
                        break;
                    case 'drop-final':
                        chunks.pop();
                        break;
                    case 'truncate-final':
                        chunks[1] = chunks[1].slice(0, -1);
                        break;
                    case 'append-record':
                        chunks.push(chunks[1]);
                        break;
                    case 'mix-revision':
                        chunks[1] = c.seal(c.random(32), { kind: 5, account: v.records[0].account_hex, object: '101112131415161718191a1b1c1d1e1f', index: 1, final: true }, new Uint8Array([1]));
                        break;
                    default: assert.fail('unknown operation');
                }
                c.decryptFile(key, v.records[0].account_hex, '101112131415161718191a1b1c1d1e1f', c.CHUNK + 1, chunks);
                return;
            }
            const r = records.get(n.base), ctx = context(r);
            let b = c.unhex(r.record_hex), key = c.unhex(r.key_hex), extra = c.unhex(r.extra_aad_hex);
            switch (n.operation) {
                case 'flip-ciphertext':
                    b[b.length - 1] ^= 1;
                    break;
                case 'flip-nonce':
                    b[47] ^= 1;
                    break;
                case 'wrong-key':
                    key = c.random(32);
                    break;
                case 'wrong-account':
                    ctx.account = c.id();
                    break;
                case 'wrong-object':
                    ctx.object = c.id();
                    break;
                case 'wrong-kind':
                    ctx.kind = 4;
                    break;
                case 'unknown-version':
                    b[0] = 2;
                    break;
                case 'short-record':
                    b = b.slice(0, 12);
                    break;
                case 'trailing-byte':
                    b = c.cat(b, new Uint8Array([0]));
                    break;
                case 'wrong-index':
                    b[41] = 1;
                    break;
                case 'wrong-final':
                    b[42] = 0;
                    break;
                case 'wrong-length':
                    b[46] ^= 1;
                    break;
                case 'invalid-final':
                    b[42] = 2;
                    break;
                case 'wrong-extra':
                    extra[26] ^= 1;
                    break;
                default: assert.fail('unknown operation');
            }
            c.open(key, ctx, b, extra);
        });
    });
test('strict metadata and Unicode', () => {
    const valid = '{"type":"folder","name":"Test","parent":null,"tags":[],"favorite":false}';
    assert.equal(c.metadata(c.utf8(valid)).name, 'Test');
    for (const bad of [valid.replace('"Test"', '"\\ud800"'), valid.replace('"name":"Test"', '"name":"a","name":"b"'), valid.replace('[]', '[null]'), valid.replace('false', 'null'), valid.replace('"folder"', '"file"'), valid.replace('"favorite"', '"Favorite"'), valid.replace('false', 'false,"size":0')])
        assert.throws(() => c.metadata(c.utf8(bad)));
    assert.throws(() => c.utf8('\ud800'));
    assert.throws(() => c.decode(new Uint8Array([255])));
});
test('account containers and recovery', () => {
    assert.equal(c.recoveryEncode(c.unhex(v.recovery_key_hex)), v.recovery_code);
    assert.equal(c.hex(c.recoveryDecode(v.recovery_code)), v.recovery_key_hex);
    for (const bad of ['', v.recovery_code + '=', v.recovery_code.toLowerCase(), v.recovery_code.slice(0, -1) + 'R'])
        assert.throws(() => c.recoveryDecode(bad));
    const { account, master, recovery } = c.createAccount('test password only');
    assert.deepEqual(c.parseAccount(c.accountBytes(account)), account);
    const unlocked = c.unlock(account, 'test password only');
    assert.deepEqual(unlocked.master, master);
    assert.deepEqual(c.open(c.recoveryDecode(recovery), { kind: 2, account: account.account, object: c.ZERO }, c.unbase64(account.recovery_wrapper)), master);
    assert.throws(() => c.unlock(account, 'wrong'));
    assert.throws(() => c.parseAccount(c.cat(c.accountBytes(account), new Uint8Array([0]))));
});
test('encrypted trash metadata roundtrip and validation',()=>{
 const key=new Uint8Array(32).fill(9),account='0102030405060708090a0b0c0d0e0f10',object='1112131415161718191a1b1c1d1e1f20';
 for(const type of ['file','folder'] as const){const m:c.Metadata={type,name:'private trash name',parent:null,tags:[],favorite:false,trashed_at:1791849600000,trash_previous_parent:'2122232425262728292a2b2c2d2e2f30',...(type==='file'?{file_id:'3132333435363738393a3b3c3d3e3f40',size:0,chunk_count:1}:{})};const data=c.encryptMetadata(key,account,object,m);assert.deepEqual(c.decryptMetadata(key,account,object,data),m);assert.equal(Buffer.from(data).includes(Buffer.from('1791849600000')),false);}
 for(const at of [-1,9007199254740992,null,1.5])assert.throws(()=>c.metadata(c.utf8(JSON.stringify({type:'folder',name:'Trash',parent:null,tags:[],favorite:false,trashed_at:at}))));
});
test('folder icon metadata uses shared valid and invalid fixtures',()=>{
 const cases=JSON.parse(readFileSync(new URL('../../testvectors/folder-icons.json',import.meta.url),'utf8'));
 const folder:c.Metadata={type:'folder',name:'public folder',parent:null,tags:[],favorite:false};
 const key=new Uint8Array(32),account='0102030405060708090a0b0c0d0e0f10',object='1112131415161718191a1b1c1d1e1f20';
 for(const folder_icon of cases.valid){const m={...folder,folder_icon};const data=c.encryptMetadata(key,account,object,m);assert.equal(c.decryptMetadata(key,account,object,data).folder_icon,folder_icon||undefined);if(folder_icon)assert.equal(Buffer.from(data).includes(Buffer.from(folder_icon)),false);}
 for(const folder_icon of cases.invalid)assert.throws(()=>c.metadata(c.utf8(JSON.stringify({...folder,folder_icon}))));
 assert.throws(()=>c.metadata(c.utf8(JSON.stringify({...folder,type:'file',file_id:object,size:0,chunk_count:1,folder_icon:''}))));
});
let failed = 0;
for (const t of cases) {
    try {
        t.run();
    }
    catch {
        failed++;
        console.error("FAIL " + t.name);
    }
}
console.log(`${cases.length - failed}/${cases.length} crypto checks passed`);
if (failed)
    process.exitCode = 1;
