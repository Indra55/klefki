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

## Optional Trash file sync

Open **Settings → Sync & devices → Trash sync** on desktop or web/PWA.

| Setting | Choices | Default |
| --- | --- | --- |
| Sync Trash for this vault | On / Off | Off |
| Sync Trash on this device | Follow vault / On / Off | Follow vault |

A device override wins over the vault default. The effective setting appears
below the controls. The device setting belongs to its local vault store and
survives restart. Changing the vault default reaches other devices when they
next contact the server.

When off, automatic uploads and downloads omit Trash file contents, including
files inside trashed folders and older revisions of currently trashed objects.
Listings, icons, removal updates, retention dates, and permanent deletions still
sync. Moving an existing document to Trash removes it from other devices' active
libraries. Turning off keeps existing copies; normal retention, permanent
deletion, and temporary-relay cleanup still apply.

When on, active documents finish before Trash contents transfer. New active work
interrupts the remaining Trash queue between files. A device setting change can
stop an ongoing Trash transfer after its current bounded chunk window; enabling
again resumes through the ordinary authenticated sync path.

Trash listings without local contents show **Not stored on this device**.
Preview and Export ask you to enable Trash sync. **Restore** explicitly requests
missing contents regardless of the receiving device's background setting, and
reports success only after the required files have been authenticated and saved.
Folder restores fetch their missing descendants first. An unavailable source
leaves the item in Trash with a retryable recovery request. Reconnect a device
that retains the file and enable Trash sync there, or restore it on that device.
This does not guarantee a recoverable copy exists elsewhere.

CLI equivalents are `sync --trash-sync follow-vault|on|off` for the persistent
device override and `sync --vault-trash-sync on|off` for the vault default.

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

Selective content delivery also requires an updated server. The first selective
sync activates a capability gate for that account; older clients receive HTTP
428 and must update. A new client refuses unsupported servers rather than
silently transferring excluded contents. Encryption envelopes, keys, and the
encrypted metadata format are unchanged.

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

## Implementation validation

Synthetic acceptance passed the complete Go test suite, targeted race checks for
selective sync/deletion/corrupt downloads, 62 browser E2E tests, 49 desktop UI
tests, 45 browser crypto checks, both frontend builds, and the Linux executable
build. Three optional model E2E tests were skipped. A complete Go race run hit
its four-minute limit during an Argon2 fixture; only the targeted race run is
counted as passing. Coverage includes default-off transfers, overrides,
multi-page history, inherited Trash membership, active conflicts, work arriving
during Trash uploads, interrupted metadata publication, restart/backup recovery,
relay reseeding, and deletion fences. Hosted rollout and physical-device
acceptance have not been performed for this change.
