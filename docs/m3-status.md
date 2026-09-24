# M3 status (2026-10-10)

M3 is implemented. No v1 cryptographic envelope, metadata format, key hierarchy,
M1 container or dependency changed. Existing uncommitted M0/M1/M2 work was
preserved; no commits were created.

## Delivered

- `core/sync`: a private SQLite local store containing encrypted metadata,
  wrapped file keys and ciphertext chunks; atomic offline edits and a persistent
  upload queue. Keys and bearer tokens remain in memory.
- `core/syncwire` and server endpoints: immutable revisions, an append-only
  account-scoped change log, transactional M2 adoption and per-device cursors.
- Ordered queued uploads, paginated chunk resume, idempotent publication,
  authenticated pull, durable local cursor updates and later server acknowledgement.
- Bounded exponential retry with cancellation and `Retry-After` support. Permanent
  authentication/protocol failures preserve queued work and stop the attempt.
- Concurrent edits become separate conflict heads. Each version and its ancestors
  remain exportable; no timestamp winner, silent overwrite or automatic deletion.
- CLI `register`, `add`, `list`, `export` and `sync`. The original container
  encryption/decryption commands continue to work independently.

## Passed checks

- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- `go -C testvectors/tools/go test ./...`
- `docker build -t cairn-vault:m3 .`

The root suite includes existing crypto/vector and CLI tests. TypeScript was not
rerun because neither the cryptographic formats nor its implementation changed.
The M3 Docker image was built; no public deployment or new container TLS smoke
run is claimed.

## Acceptance evidence

Integration tests use real TLS HTTP listeners, on-disk SQLite databases and disk
blob storage. The CLI workflow test additionally exercises explicit loopback HTTP
mode. Tests verify:

- Two devices edit the same base offline, sync, and retain both exact contents as
  conflict copies. The original revision also remains available. Queued edits
  survive local close/reopen; sessions, revisions and cursors survive server
  close/reopen.
- A multi-chunk upload exhausts retries after the first accepted chunk. Both the
  server and local client store restart; sync resumes without uploading that
  first chunk again, then the receiving device exports the exact original bytes.
- Transient chunk transport failure and a lost revision-commit response retry
  safely without duplicate visible revisions or discarded content.
- A corrupted download fails authentication, exposes no partial revision, and
  leaves the local cursor unchanged. A later clean pull succeeds.
- Eighteen queued edits exercise multi-page log replay and parent-before-child
  ordering; all empty-file revisions and a folder arrive intact.
- Offline retry exhaustion and invalid authentication leave pending work intact.
  Exponential delay, immediate stop on 401, and cancellation during a long
  `Retry-After` wait are tested.
- Failed local input rolls back, wrong keys release no output, and local stores
  reject a different account. Core clients reject unsafe origins and redirects.
- Device cursor acknowledgements are monotonic; other accounts cannot read the
  log or publish another account's records. Changed bytes under an existing
  revision ID and missing parents are rejected.
- Existing M2 ciphertext is adopted once without rewriting bytes; new M2 writes
  append a log record atomically. Reopening does not duplicate migration entries.
- Recursive scans of both devices' SQLite files/sidecars and server storage find
  no known synthetic filenames, tags, document contents, password, master key,
  login credential or session token, including their hex/base64 representations.
- CLI registration, offline add, sync between two stores, opaque-ID listing,
  selected revision export, explicit edit bases and non-overwriting export work.

## Limits

The local database encrypts sensitive records using v1 envelopes, not whole
SQLite pages. IDs, ancestry, queue flags and cursors remain visible. There is no
new authenticated history or rollback/fork defense against a malicious server.

The tested restarts are orderly close/reopen, and network faults are controlled
transport failures. These checks do not prove arbitrary power-loss survival,
all possible interleavings, Windows/Android runtime behavior, browser/device E2E
or a security audit. One process per local store is supported. Large file
transactions may block other local work, and interrupted downloads retry their
current revision rather than persisting partial downloads.

CLI network sessions are temporary and revocation on exit is best effort. Sync
is on demand; there is no watcher or background daemon. Remote recovery,
password rotation, deletion/trash, conflict merging, garbage collection, storage
quotas and M6 backup/restore acceptance remain unavailable. Retained revisions
consume storage indefinitely.

See [the sync guide](sync-protocol.md) for commands and protocol details. M4 was
not started.
