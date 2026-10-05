# M6 — Backup and safety net

Completed 2026-10-10. [Backup and restore procedure](backup-restore.md).

## Delivered

- `vault backup`: verified full encrypted export using existing `account.v1` and
  SQLite formats; a consistent snapshot includes history and pending sync work.
- `vault restore`: import into a fresh directory, authenticating all stored data
  with either the original password or the separate recovery code. Supports
  read-only backup media. Existing destinations are never overwritten.
- `vault export-plain`: all document revisions plus a manifest retaining names,
  folders, tags, favorites, trash, and revision ancestry. Opaque filenames avoid
  duplicate-name collisions and metadata path traversal.
- `vault check`: SQLite integrity, revision ancestry, every metadata/file-key
  record and every chunk, including historical revisions; rejects missing,
  reordered, modified, or orphan chunks.
- `Store.Snapshot`, `Store.Check`, and `OpenExisting` support these operations
  without changing the crypto format, key hierarchy, or dependencies.

## Evidence

- `go test -race ./...` passed across CLI, crypto, desktop service, sync, and
  server packages.
- `go vet ./...` passed.
- `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o /tmp/vault-m6.exe ./cli`
  passed. This proves compilation, not Windows runtime behavior.
- `sh scripts/test-m6-restore.sh` passed in a fresh, network-disabled Linux
  container with a read-only root and read-only backup input. The original store,
  documents, home directory, and server were unavailable to the container.
  Password and recovery-code restores matched every account byte, every row in
  `local_state`, `revisions`, `chunks`, and `sqlite_sequence`, and every plaintext
  export/manifest byte. Fixtures include a multichunk file and an empty file.
- Go tests additionally preserve folders, duplicate names, favorites, trash,
  conflicts, metadata-only edits, history, cursor and pending state; exercise
  wrong keys, corrupt/missing/reindexed/orphan chunks, invalid ancestry,
  malformed metadata/databases, missing/uninitialized stores, no-overwrite, and
  cleanup after restore failure. Export files were checked for known synthetic
  plaintext leakage.

## Boundaries

- Clean-environment acceptance is a Linux container, not a physical replacement
  machine or Windows runtime test. There was no native desktop/browser UI test
  for M6; the milestone is delivered through the CLI and shared Go store.
- Web-only work must sync into a Go store before backup. Only locally present
  data is exported. Desktop must be closed to release the store lease.
- This restores local vault state, not a lost server or its authentication and
  change log. Server binding/cursor are preserved for the same surviving server.
- Existing v1 rollback/deletion and unauthenticated sync-bookkeeping limitations
  remain. No new whole-backup authentication construction is claimed. The selected
  unlock wrapper is checked; the other account wrapper needs its own credential
  to authenticate. Recovery-only access remains local.
- Plaintext export intentionally exposes content and metadata. It is not the
  exact-import format. Use encrypted export for restoration.
- Handled errors clean up new output; abrupt termination can leave incomplete
  directories. Successful restore checks, separate backup copies, and retaining
  the previous backup remain part of the documented procedure.
- No commits were created; the repository contents were already untracked.
