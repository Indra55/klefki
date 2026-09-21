# M3 sync and offline protocol

M3 adds `core/sync`, `core/syncwire`, an append-only server change log and CLI
sync commands. It reuses the approved v1 envelopes, metadata fields and key
hierarchy without changing the M1 account or file container formats. Sync JSON
and SQLite schemas are transport/storage bookkeeping, not new crypto formats.

## Local persistence and revisions

`sync.Open(path, accountID)` opens a private SQLite database with WAL. One account
and one server URL are bound to a local database. A different account or server
URL fails closed; server relocation/rebinding is not implemented. Use separate
local databases on separate devices. Run one application process per local store;
calls within that process are serialized where needed.

Document bytes, names, folder names, tags and other private metadata are stored
only inside the existing encrypted records. SQLite pages themselves are not
SQLCipher-encrypted: opaque IDs, revision links, pending flags, server URL and
cursor are readable. No password, unwrapped key, credential or session token is
persisted. The existing encrypted account file is still needed to unlock the
master key. Core callers hold the master key in memory while unlocked and must
wipe their key copies when finished (best effort in Go).

A logical object ID is the metadata envelope's object ID. Every edit receives a
fresh random revision ID. File edits also receive a fresh file ID, file key and
nonces; encrypted metadata and its wrapped file key always travel together.
`base` names the revision the user edited, or the empty string for a new object.
A nonempty base must exist and belong to the same account/object. Revisions,
including their metadata/key pair and chunks, remain immutable.

Local `Store.Put` inserts encrypted chunks, the encrypted revision and its pending
queue entry in one SQLite transaction. Failed/short/overlong source reads roll
back the entire edit. Success means the edit is available offline and survives
an orderly process restart, even if no network is available. Nothing requires
online encryption, plaintext staging or a plaintext local index.

## Conflict copies

A head is a revision with no child revision. Editing a head replaces only that
head in the current listing; its content remains in history. Two offline edits
from the same base produce two heads. `Store.Heads` returns both, each with
`Conflict=true`; CLI `list` exposes both revision IDs with `conflict: true`.
Neither branch wins by timestamp or upload order. Editing one conflict copy
preserves the other. Multiple roots for an object likewise remain separate copies.

The copies keep their authenticated original filenames. M3 does not silently
rename encrypted metadata or merge content. Callers select/export a copy by
revision ID. Deletion, trash, branch resolution and revision garbage collection
are not implemented; all historical ciphertext is retained.

The revision graph, sequence numbers and cursors are server-visible bookkeeping,
not cryptographically authenticated history. Envelope account/object/file binding
and whole-file authentication still apply. A malicious server can hide, replay or
alter history relationships; M3 adds no rollback/fork protection beyond the
approved v1 threat model.

## Server endpoints

All endpoints require the M2 device session bearer token. JSON binary fields use
base64; IDs are nonzero lowercase 32-hex strings. Existing TLS, size and rate
limits apply. No request contents are logged.

| Method and path | Contract |
|---|---|
| `PUT /v1/sync/revisions/{id}` | JSON `{id, object, base, metadata, file_key?}`. Publishes a metadata/key pair to the append-only log. 201 for a new revision; 200 for an identical retry; response `{sequence}`. Changed bytes/links under an existing ID or an unavailable/wrong-object base return 409. |
| `GET /v1/sync/changes?after=N` | `{changes:[{sequence, revision:{...}}]}` in increasing sequence order, at most 16 records. Empty array means caught up at that instant. A cursor greater than the account's current maximum returns 409. |
| `GET /v1/sync/cursor` | `{sequence}` for the authenticated device, initially 0. |
| `PUT /v1/sync/cursor` | `{sequence}` acknowledges durable local progress; 204. Acknowledgements never decrease. Negative or beyond-account-maximum values return 400. |

Sequence numbers are SQLite integers and may have gaps because the log is shared
by accounts. Queries and base lookups are always account-scoped. Other accounts'
records are never returned. The server retains cursor rows after session revocation;
there is no log pruning based on acknowledgements.

Startup transactionally adopts existing M2 metadata records as root revisions,
using their metadata ID as revision ID, exactly once. Ciphertext is unchanged.
Future M2 `PUT /metadata/{id}` calls publish their original immutable record and
log entry atomically. The M2 metadata endpoints remain a legacy immutable view;
M3 callers use the change log to see edits and conflicts. Sync revisions do not
rewrite the old metadata table.

## Push, pull, retry and restart

1. Pending edits are pushed in local insertion order, so a queued base precedes
   its children. The client verifies local file integrity before uploading.
2. It pages through the server's chunk inventory and uploads only missing chunks.
   Retries use the exact stored ciphertext. M2's exclusive publication makes an
   identical retry safe, including after a lost HTTP response or server restart.
3. Only after all chunks are accepted does it publish the immutable revision.
   The server transaction assigns one log sequence. Repeating a lost commit
   response returns that same sequence. Only then is the local pending flag cleared.
4. Pull starts from the **local** durable cursor, never from the server's device
   acknowledgement. For each event the client authenticates metadata, its file
   key wrapper, and every chunk's account/file/index/final flag/length. Missing,
   truncated or modified chunks fail the pull.
5. Complete ciphertext, the revision and the new local cursor commit in one
   transaction. A failed import leaves no partial visible revision and does not
   move the cursor. Earlier successfully imported revisions remain durable.
6. Device acknowledgement follows that local commit. Lost acknowledgements can
   be retried or sent on the next sync. A freshly created local store starts at
   zero even if the supplied session already has a later server acknowledgement.

Each sync request retries network failures, HTTP 408, 429 and 5xx up to four
attempts by default, with exponential delays starting at 250 ms and capped at
30 seconds. Integer `Retry-After` values up to 120 seconds are honored even when
longer than the backoff cap. All waits are cancellable. Authentication failures,
409 conflicts and malformed/authentication-failing data stop immediately. Retry
settings are configurable in `Client.Retry`; attempts are capped at ten.

After exhaustion, durable pending work remains for the next `Sync` call. There
is no background daemon or filesystem watcher. The current CLI runs one sync
pass on demand. Successful upload plus failed pull still preserves the local
revision; the log is replayed next time. Downloads commit at revision granularity:
a failed multi-chunk download retries that revision, while uploaded chunks resume
individually. The client bounds response sizes, rejects redirects (including
same-origin redirects), and uses normal certificate validation.

## Core API and CLI

Core entry points: `Open`, `Store.Put`, `Store.Heads`, `Store.Export`,
`Store.Pending`, `Store.Cursor`, `NewRemote`, `Remote.Signup/Login/Revoke`, and
`Client.Sync(context)`. `Store.Put` supports files and folders. For file edits,
the caller supplies the full new content and size. `Store.Export` authenticates
the complete file before its output pass, both in one database snapshot; callers
should still stage output to handle writer failures. CLI export uses the existing
private, non-overwriting publication helper.

Build the CLI with `go build -o vault ./cli`. After `vault init`, for development:

```sh
# In a separate terminal; loopback development only.
go run ./cmd/vault-server -dev-http

# Register the already-created encrypted account once.
./vault register --server http://127.0.0.1:8080 --dev-http

# Works offline; prints opaque object and revision IDs.
./vault add /path/to/document.pdf
./vault list

# When connected, push queued work and pull authenticated revisions.
./vault sync --server http://127.0.0.1:8080 --dev-http

# Edit the selected revision, including when offline.
./vault add --object OBJECT_ID --base REVISION_ID /path/to/edited.pdf

# Export either conflict copy by its revision ID; never overwrites output.
./vault export -o /path/to/restored.pdf REVISION_ID
```

Flags precede positional arguments. All commands support `--vault PATH` and
`--password-file PATH`; passwords are otherwise prompted without echo. Local
commands and sync accept `--store PATH`, defaulting to `local.db` alongside the
account file. `list` deliberately prints only IDs and conflict flags, not private
filenames. The older `encrypt`/`decrypt` container commands remain independent of
the sync store; no automatic import/export of those containers is claimed.

For another device, securely copy the **encrypted account file**, use its password,
and start with a fresh local store. Register only once; sync logs in to the same
account. Use a stable HTTPS origin and a certificate trusted by each client for
normal operation. Plain HTTP is accepted only with `--dev-http` and a literal
loopback IP.

The CLI creates a temporary session for each network command and attempts to
revoke it on exit with a five-second timeout. If the network fails or the process
is killed, that session remains until revocation or normal server expiry. The
local cursor persists across these sessions. Core callers can retain a session
in memory for a longer-lived device connection. Persistent OS-keystore credential
storage and remote account recovery remain outside M3.

## Limits

M3 tests cover specified failure scenarios; they do not prove survival of every
hardware/power failure, malicious server behavior or concurrent multi-process use
of a local store. No encrypted SQLite page layer, revision garbage collection,
server quotas, remote recovery, backups, platform UI or browser interoperability
is added here. SQLite transactions can be long for large files, and pulling a
revision holds the local write transaction while downloading it. M6 restore and
backup acceptance is still required before heavy real use.
