# Backup and restore

Build the CLI with `go build -o klefki ./cli`. M6 operates on the Go local store
used by desktop and CLI. Close the desktop app before opening its store with the
CLI. Use its actual account and store paths; the defaults below are
`.klefki/account.v1` and the sibling `local.db`.

## Create an encrypted backup

Sync all devices first if their newest work should be included. A backup contains
everything in this local store, including offline pending edits, all revisions,
conflict copies, folders, tags, favorites, and trash. It cannot include changes
that have never reached this store.

```sh
./klefki check --klefki /private/klefki/account.v1 --store /private/klefki/local.db
./klefki backup --klefki /private/klefki/account.v1 --store /private/klefki/local.db -o /backups/klefki-2026-10-10
```

Both commands prompt for the existing password. Automation can use
`--password-file PATH`. Local recovery can use `--recovery-file PATH` instead;
passwords and recovery codes are never command-line values.

Keep the **whole output directory** together. `account.v1` is the existing VLTA
v1 wrapped-account container; `local.db` is a consistent snapshot of the existing
encrypted SQLite store. This introduces no new encryption or account format.
SQLite structure, opaque IDs, sync cursor, pending flags, sizes, and the remote
URL remain visible, just as in the local store. Document content, names, tags,
folder metadata, and file keys remain encrypted. An empty `.lock` file may also
be present; it contains no state and need not be retained.

Store the recovery code separately. The backup does not include it, a password,
an unwrapped key, a device session, fingerprint credentials, or app preferences.
Keep another copy on separate storage. Treat a directory as complete only when
the command exits successfully. Handled failures remove the new directory;
process termination or power loss can leave an incomplete directory. Keep the
previous backup until the replacement has been successfully restored and checked.

## Restore on a clean machine

1. Install/build the CLI. Copy the completed backup and obtain either its original
   password or its separate recovery code. No server, previous installation,
   desktop settings, or original documents are needed.
2. Choose an output directory that does not exist. Its parent must already exist:

   ```sh
   ./klefki restore -o /private/restored-klefki /backups/klefki-2026-10-10
   ./klefki check --klefki /private/restored-klefki/account.v1
   ./klefki list --klefki /private/restored-klefki/account.v1
   ```

   Or recover locally without the password:

   ```sh
   ./klefki restore --recovery-file /private/recovery.txt -o /private/restored-klefki /backups/klefki-2026-10-10
   ./klefki check --klefki /private/restored-klefki/account.v1 --recovery-file /private/recovery.txt
   ```

3. `restore` verifies all metadata, wrapped file keys, and every chunk of every
   revision before reporting success. Existing destinations are never replaced.
   It accepts completed backup directories, including read-only media; do not
   point it at a live store or a manually copied database with a WAL/journal.
4. Open the restored `account.v1` and sibling `local.db` in the desktop app, or
   continue using the CLI with `--klefki` pointing at the restored account.
   Reconnect to the same surviving server using the original password. The
   cursor, server binding, and pending queue are preserved. Recovery-code unlock
   remains local-only; it does not reset online authentication.

This restores local klefki state exactly, not the server's users, sessions, or
change log. Rebuilding a lost server or moving to another server is not an M6
operation; do not reuse a preserved cursor against a newly initialized server.

## Plaintext export

```sh
./klefki export-plain --klefki /private/restored-klefki/account.v1 -o /private/plaintext-export
```

This deliberately writes **unencrypted** documents and metadata, including trash,
conflicts, and historical versions. Each file revision is written under its
opaque revision ID. `manifest.json` (version 1) records the account ID and ordered
revisions with object/revision/base IDs and complete decrypted metadata. Folder
revisions have manifest entries without content files. The parent IDs and base
links preserve folder relationships and revision history. Original names are
metadata, never filesystem paths, so duplicate names cannot overwrite files.

Use the manifest's `Revision` field to locate each file and `Metadata.name` to
identify its original name. This is an inspection/portability export; use the
encrypted backup for exact import. Plaintext re-import is not provided.

## Integrity boundaries and verification

`check` runs SQLite's integrity check, validates revision ancestry, decrypts every
metadata record and file-key wrapper, verifies each chunk's account/file/index/
final flag and plaintext length, and rejects missing or orphan chunks. This covers
historical and trashed data, not just current heads. It prints only counts.

As documented for v1, valid old backups and deletion of whole branches cannot be
detected cryptographically. Sync bookkeeping and revision edges are not an
authenticated whole-klefki manifest. A successful check is not proof of freshness
or that another device has no unsynced work. Only the account wrapper used for
unlock is authenticated; periodically test recovery-code restoration separately.

The browser's IndexedDB store cannot be supplied directly to these commands.
Sync web changes to a desktop/CLI store before backing them up. M6 supplies CLI
operations; desktop/web backup buttons are not included.

Reproduce clean-environment acceptance with:

```sh
sh scripts/test-m6-restore.sh
```

Requires Go, Python 3, Docker, and the local `klefki:m3` image (or set
`M6_TEST_IMAGE` to an available Linux image with `/bin/sh`). It uses synthetic data,
mounts only the binary, backup and test credentials into a fresh network-disabled
container, restores via both unlock methods, and compares every document byte,
manifest, account byte, and row of all local-store tables. This is isolated Linux
container evidence, not physical-machine or Windows runtime acceptance.
