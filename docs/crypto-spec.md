# Vault cryptographic format v1

Status: reviewed and approved for M1 by the user on 2026-10-09. M0 tooling
exercises these formats; it is not a security audit. Any later change to these
formats or the key hierarchy requires user approval.

## Conventions and limits

All offsets below are zero-based, lengths are bytes, integers are unsigned
big-endian. Concatenation is `||`. ASCII labels include the explicit terminating
NUL (`\0`). Binary fields are raw bytes, not their hex strings. JSON fixtures use
lowercase hex; JSON is not a cryptographic wire format. Unknown versions, kinds,
flags, length mismatches, and trailing bytes are errors. No permissive fallback.

Account IDs and object IDs are 16 random bytes. The all-zero object ID is reserved
for account master-key wrappers. Other IDs must be nonzero. File ID identifies
a file; each new content revision receives a fresh independent file key. A
metadata object's identity is supplied by its caller, never trusted solely from
the received record. Key sizes are always 32 bytes; AEAD tags are 16 bytes.

Chunk plaintext is at most 4,194,304 bytes. Metadata plaintext is at most
1,048,576 bytes. The v1 client-supported file size limit is 2^53-1 bytes, so Go
and JavaScript can represent size/count metadata exactly. Chunk index occupies
8 bytes but values must be below the expected chunk count. Validate sizes and
KDF parameters before allocation or expensive work. These are format limits,
not upload quotas; server request limits belong to M2.

## Password derivation and authentication

Passwords are Unicode scalar values encoded as UTF-8, without normalization,
trimming, case conversion, or a terminating NUL. Reject invalid UTF-8/unpaired
surrogates instead of replacement decoding. The input limit is 1024 UTF-8 bytes.
An empty password is a KDF test case, not a permitted signup password. Password
strength policy and interactive enrollment belong to M1/M2.

The account stores this 27-byte descriptor `D`:

| Offset | Length | Meaning |
|---|---|---|
| 0 | 1 | Descriptor version: 1 |
| 1 | 1 | Algorithm: 1 = Argon2id v1.3 (0x13) |
| 2 | 4 | Memory: 65,536 KiB |
| 6 | 4 | Iterations: 3 |
| 10 | 1 | Parallelism: 1 |
| 11 | 16 | Random per-account salt |

The initial supported parameter profile is exactly the tuple above. Reject other
profiles before running the KDF; do not downgrade on low memory. Broader tuning
profiles require review and cross-language vectors. This initial profile has not
been benchmarked on target browsers or mobile devices.

`K = Argon2id(password_utf8, salt, t=3, m=65536 KiB, p=1, output=64)`.
`KEK = K[0:32]`; `auth_key = K[32:64]`. Go uses `argon2.IDKey`.
TypeScript uses `crypto_pwhash(64, ..., 3, 67108864, ALG_ARGON2ID13)`;
select the explicit algorithm, never the changeable default. Libsodium's memory
argument is bytes and its Argon2id parallelism is one.

`credential = HMAC-SHA256(auth_key, "vault/auth/v1\0" || account_id)`.
`verifier = SHA256("vault/auth-verifier/v1\0" || credential)`.

The client sends the credential only over authenticated TLS. The server stores
the verifier, compares candidate verifiers in constant time, and never accepts
the verifier itself as a login credential. A captured credential is replayable
for login; it does not yield the KEK. This is bearer authentication, not a PAKE.
The descriptor, account ID, and wrapped master key are needed before unlock;
server endpoint discovery, enumeration defenses, sessions and throttling are M2.
The verifier and wrapped master key permit offline password guessing after a
breach; Argon2id increases its cost, not its impossibility.

## Common encrypted envelope

Use the library XChaCha20-Poly1305 IETF combined mode, whose result is ciphertext
followed by its 16-byte tag. Use a fresh random 24-byte nonce on every encryption.
Never reuse fixture nonces or deterministic fixture generation in production.

| Offset | Length | Meaning |
|---|---|---|
| 0 | 1 | Format version: 1 |
| 1 | 1 | Kind: see below |
| 2 | 16 | Account ID |
| 18 | 16 | Object ID |
| 34 | 24 | Nonce |
| 58 | variable | Ciphertext followed by tag |

For kinds 1–4, header `H` is bytes `[0:58]`.
`AAD = "vault/record/v1\0" || H || extra`.
`record = H || AEAD.Encrypt(key, nonce, plaintext, AAD)`.
Caller supplies the expected account, object, and kind; check all of them.

| Kind | Purpose | Encryption key | Plaintext | Object | Extra AAD |
|---|---|---|---|---|---|
| 1 | Password master wrapper | KEK | Master key | Zero | Exact descriptor D |
| 2 | Recovery master wrapper | Recovery key | Master key | Zero | Empty |
| 3 | File-key wrapper | Master key | File key | File ID | Empty |
| 4 | Metadata | Master key | UTF-8 JSON bytes | Metadata object ID | Empty |

Kinds 1–3 must contain exactly 32 plaintext bytes, making each record 106 bytes.
Master, recovery, and file keys are independently random. Purpose separation is
provided by authenticated record kinds. A kind-3 record cannot be accepted as
metadata or another wrapping purpose, even though its encryption key is shared.
The password wrapper authenticates the complete KDF descriptor. Corrupting that
descriptor must fail validation or authentication, not trigger a fallback.

## Chunk envelope (kind 5)

| Offset | Length | Meaning |
|---|---|---|
| 0 | 1 | Format version: 1 |
| 1 | 1 | Kind: 5 |
| 2 | 16 | Account ID |
| 18 | 16 | File ID |
| 34 | 8 | Zero-based chunk index |
| 42 | 1 | Final flag, exactly 0 or 1 |
| 43 | 4 | Plaintext length |
| 47 | 24 | Nonce |
| 71 | length + 16 | Ciphertext followed by tag |

`H = record[0:71]`; `AAD = "vault/record/v1\0" || H`.
Encrypt with that content revision's file key. A whole record is exactly
`87 + plaintext_length` bytes. Framing is intrinsic: read the fixed header,
validate the bounded length, then read exactly length + 16 bytes.

For a file of length L, chunk count is `max(1, ceil(L / 4194304))`.
All non-final chunks are full-sized. The last chunk alone has final=1.
For an exact multiple of chunk size, the last full chunk is final; do not append
an empty chunk. An empty file consists of one index-0, final=1, zero-byte chunk.
No other empty chunks are valid.

The whole-file reader checks consecutive indices, the expected account/file ID,
exactly one final marker, EOF immediately after the final record, and agreement
with the authenticated metadata's length/count. Missing chunks, truncated tags,
reordering, or extra records are errors. Each chunk must authenticate before
release; a successful individual chunk does not prove the whole file complete.
A CLI decrypt must stage output privately and publish it only after complete
validation. Do not leave a partially decrypted destination on failure.

Never reuse a file key for changed file content. Retries reuse the already
produced ciphertext; re-encryption uses fresh nonces. A different revision's
chunk fails under the current revision key. Replaying a complete older metadata,
wrapped key, and chunk set remains outside v1 rollback protection.

## Metadata bytes and key association

Metadata JSON is UTF-8 without a BOM. Exact bytes are encrypted; there is no
canonical-JSON requirement and decryption never reserializes before verifying.
Reject duplicate member names, invalid Unicode, and malformed JSON after AEAD
verification. Metadata carries a `type` of `file` or `folder`, `name` (string),
`parent` (null or lowercase 32-hex metadata object ID), `tags` (array of strings),
and `favorite` (boolean). File metadata additionally carries `file_id` (lowercase
32-hex), `size` (integer from 0 through 2^53-1), and `chunk_count` (the formula
above). File names, folder names, tags, and all these metadata fields remain
inside the encrypted payload. UI path/name constraints belong to later clients.

The file-key wrapper belongs to the revision selected with that metadata. Its
expected file ID comes from decrypted metadata. Metadata and the wrapped file key
must be stored/updated atomically as one revision in later sync work; the server
can deny access or replay a complete revision. Fresh file keys prevent mixing
chunks, not rollback or a malicious server hiding whole files. Sync wire APIs and conflict records remain outside M1. The approved CLI
container layout is specified below.

## Recovery and password changes

Encode the random 32-byte recovery key using RFC 4648 uppercase Base32 without
padding, prefixed with literal `vlt1-`: exactly 57 ASCII characters. Accept only
the canonical representation: exact prefix, 52 alphabet characters, zero pad
bits; reject whitespace, alternate case and padding. No checksum is included;
a mistyped but syntactically valid code fails master-wrapper authentication.
This is an encoding, not a password; never run it through the password KDF.

Recovery decrypts the kind-2 master wrapper locally. The recovered master key
must match the one produced through the password wrapper and unwrap the same
file keys. A password change uses a new salt/KEK, rewraps the existing master key,
and derives a new auth credential. Do not re-encrypt all document chunks.

Possessing a recovery key does not currently authenticate to the server. M1
recovery is local, using already available account records and ciphertext. A
remote password reset authorization flow must be specified and reviewed in M2
before claiming online account recovery. Never send the recovery key to a server.
Rotating a password cannot revoke plaintext or keys previously acquired by an
attacker. Server device revocation likewise does not erase offline copies.

## Randomness, secrets and implementation boundary

Use `crypto/rand` in Go and libsodium `randombytes_buf` in TypeScript. All salts,
IDs, master/recovery/file keys and nonces are generated with a CSPRNG. Use library
constant-time secret comparisons and best-effort memory wiping, while documenting
that Go/JS runtimes can retain copies. No key/password/document logging, telemetry
or crash attachments. Public synthetic vector secrets are the only committed
exception and must be clearly labelled. Fixed fixtures must never enter runtime
code or be offered as defaults.

M0 tools only verify byte layouts, primitives, and representative rejection
rules. They are deliberately not production parsers, metadata validators, or a
security audit. M1 must write tests before implementing the crypto core and add
bounded parsers plus fuzz coverage, including resource-exhaustion cases.

## References

- [Argon2 v1.3 (RFC 9106)](https://www.rfc-editor.org/rfc/rfc9106.html)
- [Libsodium password hashing](https://libsodium.gitbook.io/doc/password_hashing/default_phf)
- [Libsodium Argon2id parameter mapping](https://github.com/jedisct1/libsodium/blob/master/src/libsodium/crypto_pwhash/argon2/pwhash_argon2id.c)
- [XChaCha20-Poly1305 API](https://libsodium.gitbook.io/doc/secret-key_cryptography/aead/chacha20-poly1305/xchacha20-poly1305_construction)
- [JavaScript sumo package](https://github.com/jedisct1/libsodium.js#standard-vs-sumo-version)
- [Base32 (RFC 4648)](https://www.rfc-editor.org/rfc/rfc4648.html)

## M1 local CLI containers (approved 2026-10-09)

These containers package the existing records without changing their encryption
or key hierarchy. All integers remain big-endian. No plaintext name is stored in
an outer header. No optional sections or padding are accepted in v1.

Account file (exactly 260 bytes): `ASCII("VLTA") || 0x01 || account_id[16] ||
D[27] || password_master_record[106] || recovery_master_record[106]`. Both record
headers must agree with the account ID, zero object ID and expected kind.
Account file EOF must follow immediately. Store only this wrapped account state;
never persist a master key or password in the account file.

Encrypted file: `ASCII("VLTF") || 0x01 || account_id[16] || file_id[16] ||
metadata_id[16] || file_key_record[106] || metadata_record_length:u32 ||
metadata_record || chunk_records...`. The fixed prefix is 163 bytes. The metadata
record length is 74 through 1,048,650 bytes. Check it before allocation. Validate
account ID against the selected local account, decrypt metadata using metadata
ID, require type=file and equality between decrypted file_id and the outer file
ID, then unwrap the file key with the same file ID. Use authenticated metadata
size/count for the complete stream checks. The final chunk must be followed by
EOF. These containers carry no independently authenticated history or rollback
protection beyond the records already specified.

CLI init writes the recovery code once to an explicit user-selected private file;
it is not printed into logs or ordinary stdout. Treat that file as sensitive and
store it separately from the encrypted vault. Local decryption may use this code
instead of the password. Secret file inputs remove one optional trailing LF or
CRLF; other bytes, including leading/trailing spaces, are preserved. Interactive
password input hides terminal echo and confirms the password at init. No password
or recovery-code command-line value or environment-variable option is provided.

Encrypted output defaults to an opaque file ID plus `.vault`. Plaintext output
requires an explicit destination; never treat a decrypted filename as a path.
Both operations stage a new private file in the destination directory, sync and
close it, then publish with a non-overwriting hard link. Delete the staging name
on success or ordinary error. Filesystems without hard-link support fail closed.
An OS/process crash can leave a private staging file; crash-safe cleanup and
Windows ACL guarantees are not claimed by M1.
