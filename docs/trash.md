# Trash and permanent deletion

Move documents or folders to Trash to keep them recoverable for **30 days**.
Desktop and web/PWA show each item's deletion date. Folder contents inherit the
folder's deadline. Restore cancels the deadline; trashing again starts a new one.
Existing trash receives a fresh countdown when an updated client initializes it.

Use **View → Trash → Actions → Delete permanently** to remove one item, or
**Empty Trash** to remove everything there, including items hidden by the current
search. Both actions confirm the count and remove folder contents, historical
versions, and conflict copies. Permanent deletion cannot be undone in Klefki.
Automatic cleanup skips unresolved conflicts, including conflicted ancestors.

Cleanup runs on unlock and periodically while a client is unlocked. Connected
vaults require a successful sync before automatic deletion. Offline manual
deletion removes the local copies and queues deletion for the next sync. Closed
apps and offline devices keep their copies until they reopen and sync.

## What is removed

Deletion removes the object's encrypted revisions, metadata, wrapped file keys,
and unreferenced encrypted content chunks. Affected historical keyword/semantic
indexes and correction records are also removed. Desktop rebuilds indexes from
surviving documents when document-text search next needs them; rebuilt indexes
use new object IDs. Browser search can use a subsequently synced desktop index.
Shared chunks still referenced by surviving objects are retained. Local SQLite
stores are checkpointed and compacted; browser disk reclamation follows
IndexedDB's storage management. Server blob cleanup is retried from a durable
queue after an interruption.

A small deletion ledger remains, containing opaque object/file IDs, whether an
object was a folder, and delivery bookkeeping. Trash dates and previous parents
remain encrypted. Deletion records reach other devices before ordinary history
replay, preventing old revisions and offline restores from bringing deleted
objects back. Children later discovered inside a permanently deleted folder are
also permanently deleted. Independently moved children outside that folder are
preserved.

## Server compatibility and backups

Deploy the updated server before updating clients. Until a connected server
supports deletion, manual permanent deletion asks for an update and automatic
cleanup remains disabled. After the first deletion reaches the server, that
account requires deletion-aware clients for sync. This applies in persistent and
temporary-relay modes. Already downloaded copies on old clients cannot be erased
remotely; those clients must update and reconnect.

New encrypted backups preserve deletion records. An older backup reconnecting
to a surviving server ledger applies deletion before uploading its history.
Independent backups and plaintext exports are not rewritten, and can still be
opened offline. If the server loses all state, a surviving updated local store
can supply its deletion ledger before replaying retained history. If every copy
of the relevant ledger is lost, deletion cannot be reconstructed. Keep server
control state durable for reliable protection across server loss.

Deletion is application-level storage removal, not forensic disk erasure or a
promise to erase host snapshots, operating-system caches, or external backups.
Like other sync bookkeeping, deletion records trust authenticated clients and
the server; they do not add cryptographic rollback protection.

## Protocol extension

Updated clients send `X-Klefki-Deletion: 1`. Existing response shapes remain
unchanged for older clients until deletion is enabled for their account. Older
clients then receive HTTP 428, and legacy metadata endpoints are unavailable.
Deletion-aware settings expose `deletion_supported` and `deletion_sequence`;
status exposes `deletionSequence` when nonzero.

- `GET /v1/sync/deletions?after=SEQUENCE` returns `{epoch, latest, enabled,
  changes:[{sequence, deletion:{object, folder, files}}]}`. Pages contain at most
  16 records. The cursor is separate from the ordinary revision cursor.
- `POST /v1/sync/deletions` accepts `{deletions:[{object, folder, files, heads?}]}`.
  `heads` is the expected revision-head set for automatic deletion. A changed
  set returns 409 without deleting any target in the request. Manual deletion
  omits it. Requests allow at most 256 objects and are idempotent.
- Deleted objects and retired blobs reject publication with HTTP 410. Ordinary
  change-log entries keep identities and cursor ordering while their encrypted
  metadata/key payloads are cleared. Updated clients skip those entries using
  their deletion ledger.

Local SQLite migrations add ledger/control tables without rewriting document
ciphertext. IndexedDB upgrades the existing database to version 2 and preserves
its account, revisions, and chunks. Encryption envelopes and key derivation
remain v1.

## Verification

Verified locally on 2026-10-13: Go race checks and vet, 44 crypto checks,
37 desktop UI tests, 44 Chromium scenarios, both production frontend builds,
and the Linux desktop executable. Scenarios include permanent deletion in both
sync modes, stale-backup recovery, automatic expiry, shared chunks, interrupted
server cleanup, large folders, existing IndexedDB migration, and offline deletion.

See the focused deletion tests in `core/sync/deletions_test.go`, desktop UI tests,
and Go/browser deletion cases in `web/e2e/klefki.spec.ts`. These use synthetic
fixtures. Hosted deployment, native-window interaction, and an installed mobile
PWA require separate acceptance.
