# Klefki threat model

Klefki protects personal documents using client-side authenticated encryption.
M0 through M3 now implement the formats, local CLI, ciphertext server and offline
sync. Tests establish specific behavior, not a security audit.

## Assets and trust boundaries

Protect document bytes, names, folder names, tags, extracted text, search indexes,
passwords, KEKs, master keys, recovery keys, and file keys. Encryption and
plaintext processing happen only on trusted clients. Client downloads are
untrusted until their records authenticate and whole-file checks complete.

The server stores ciphertext, wrapped keys, public KDF descriptors, opaque IDs,
auth verifiers and operational session/sync information. "Only ciphertext" means
only ciphertext for document content and private metadata, not that every server
column is encrypted. IDs, ciphertext sizes, timestamps, account/device counts,
IP addresses, access patterns and sync activity may be visible. Padding and
traffic-analysis resistance are not part of v1.

## Defenses and limits

| Adversary/event | Intended protection | Remaining limit |
|---|---|---|
| Stolen server disk or database snapshot | Files, names and private metadata remain encrypted; keys are wrapped | Weak passwords can be guessed offline against wrappers/verifiers |
| Server operator reading storage | No plaintext documents or decryption keys available | Sizes, timing and opaque relationships can leak |
| Network attacker | Authenticated TLS protects transport and bearer credentials; AEAD protects stored content | Incorrect TLS validation or a compromised client defeats this boundary |
| Modified, swapped or truncated ciphertext | AEAD context binds kind, account, object, chunk order and final status; whole-file checks reject incomplete files | Availability cannot be guaranteed |
| Mixed chunks from content revisions | A fresh independent file key per content revision causes authentication failure | A complete prior valid revision can still be replayed |
| Offline encrypted store theft while locked | Same ciphertext/key-wrapping model as remote storage | Password guessing, OS caches and implementation mistakes remain risks |

A missing final chunk is detectable when opening a known file. A server hiding
an entire file, returning an old valid listing, rolling back all records, or
forking views between devices is not reliably detectable in v1. No trusted
monotonic history or anti-rollback checkpoint protocol is claimed.

## Excluded threats

- Compromised endpoint, keylogger, malicious extension, screen capture, or an
  attacker reading unlocked process memory. Go/JavaScript zeroization is best
  effort and cannot erase every runtime copy.
- A malicious server serving modified web JavaScript or service-worker updates.
  A PWA receives executable code from its origin; E2EE does not independently
  authenticate that code. A compromised web delivery origin can steal secrets
  on the next unlock. Desktop distribution/update trust is also required.
- Plaintext exports, external PDF viewers, clipboard contents, thumbnails,
  temporary files, crash dumps, swap, and user-created backups outside the klefki.
  Later clients must minimize these surfaces; M0 implements none of them.
- Deletion, denial of service, server loss, and rollback. Encrypted backups and
  tested restores are provided by [M6](backup-restore.md); replication alone is
  not a backup. Existing rollback/deletion limitations still apply to backups.
- Protection after a recovery code, password, or unlocked keys have been stolen.
  Server device revocation prevents future authorized server access, not access
  to already downloaded ciphertext for someone holding its keys.

## Authentication and recovery

The TLS login credential is a domain-separated derivative of the auth key,
separate from the KEK. A stolen credential permits server login and potential
data destruction but does not directly decrypt files. The server retains a
verifier, not the bearer credential; online rate limits and session controls
belong to M2. A malicious server can capture credentials received during login.

A random recovery key unwraps the master key locally. It is not sent to the
server and is not yet a remote account reset protocol. Recovery requires the
wrapped master record and ciphertext to remain available. Losing both password
and recovery key leaves no intentional decryption backdoor. Losing the ciphertext
cannot be repaired by possessing a key.

## Evidence and future review

M0 fixtures check interoperable primitives, record bytes and representative
rejections. They do not prove production parser safety, safe temporary-file
handling, TLS configuration, web-origin integrity, hardware behavior, or restore
correctness. M1 adds production code tests and parser fuzzing; later milestones
must establish their own acceptance evidence. The user approved this threat model
and the crypto spec for M1 on 2026-10-09. No external cryptographic audit has
been performed.

## M3 local storage and sync boundary

Local SQLite stores existing encrypted envelopes and ciphertext chunks, while
opaque IDs, revision links, queue flags, server origin and cursors remain visible.
Keys and tokens stay in client memory. The revision graph and server change log
are not authenticated history; replay, omitted changes and forged relationships
remain outside v1 rollback/fork protection. Honest-server concurrent edits keep
both heads as conflict copies. See [M3 evidence](m3-status.md) and the
[sync protocol](sync-protocol.md) for tested restart/retry scenarios and limits.
