# PWA offline storage and folder recovery

Downloaded documents live in encrypted IndexedDB on this device. Opening them
must not require server authentication, an active relay, or another device.
Sync updates copies; it is not the mechanism for opening an already saved file.

Klefki requests browser storage persistence during setup/unlock. Check **Settings
→ Offline & backup** for the actual result. Denial does not prevent offline use,
but the browser may evict unprotected storage. Persistence does not prevent
explicit site-data clearing, profile removal, or loss of the device.

## Choose a recovery folder

On a browser exposing directory read/write access, choose **Choose offline backup
folder**. Klefki creates `klefki-<account ID>` inside that location. Keep that
whole subfolder. Use a separate location for each browser profile; browser locks
coordinate writers within one origin/profile, not separate profiles sharing a folder.
Folder access is optional; browsers without this capability
show its absence instead of promising a folder backup.

The initial backup runs immediately. Later changes are coalesced for five seconds,
with at most thirty seconds of debounce while open and unlocked. New downloads,
offline edits, deletion records and preference changes are included. Jobs run
serially. Closing or locking the PWA can interrupt a job; the previous completed
snapshot stays available. There is no promise of writes while the app is closed.

The status distinguishes files available offline, browser storage protection,
and the last completed folder backup. **Back up now** also requests folder access
again if its permission expired. Permission denial, missing files and disk errors
leave changes pending. Do not assume a pending or incomplete backup contains the
latest documents. An unexpectedly empty or incomplete store cannot automatically
replace a complete backup; **Review incomplete backup** requires an explicit choice.

## Restore without another device

1. Open Klefki and choose **Restore from backup folder** on the unlock screen or
   in **Settings → Offline & backup**.
2. Select the `klefki-<account ID>` subfolder, containing `account.klefki`.
3. Enter the account password and choose a completed snapshot. If the latest
   catalogue is damaged, an earlier verified catalogue remains selectable.
4. Restore and unlock. Every included file is authenticated before the restored
   store becomes active. The previous local store is retained on failure and
   after a successful switch. Sync starts paused.
5. Resume sync when ready. Restored clients use a fresh device identity and zero
   delivery cursors; existing deletion reconciliation runs before republishing.

Restoration works offline once the application shell is available. Clearing all
site data may also remove the offline application shell, requiring the app to be
loaded again before restoration. The folder's document data does not require a
server or peer. An older independent backup can contain documents subsequently
deleted elsewhere. A folder on the same phone cannot protect against losing that
phone; keep a separate copy for that risk.

## Browser backup format v1

This is a PWA-specific container, not the desktop/CLI SQLite backup format. It
reuses the existing account and encrypted record formats without a new cipher,
key hierarchy, sync endpoint or dependency.

- `account.klefki`: the existing 260-byte wrapped account container.
- `<file ID>-<chunk index>.bin`: immutable encrypted file chunks. No plaintext
  names or document contents are written to the folder.
- `snapshot-<time>-<random ID>.json`: a small commit envelope identifying the
  account, random catalogue file ID, wrapped file key, length and timestamp.
- The catalogue is a normal encrypted file, with a random file key wrapped using
  record kind 3 and its contents encrypted as kind 5 chunks. It records version,
  revisions/history, pending state, deletion ledger, selected device preferences,
  cached account policies, and the size/SHA-256 inventory of document chunks.

Catalogue plaintext is bounded to 64 MiB and parsed strictly. Chunk copies and
verification are incremental. Snapshot metadata is captured in one IndexedDB
transaction; if concurrent cleanup removes a required chunk, backup fails without
publishing a new snapshot. The commit envelope is written last. Each completed
catalogue and every included document chunk is verified before restoration.
The newest two completed snapshots are retained; old chunks are collected only
when no retained snapshot references them. Invalid/unknown snapshot files stop
cleanup so they can be investigated. A catalogue is not proof against replay of
an entire old, otherwise authentic backup.

Backups include cached relay/Trash policy, device Trash override, sync pause,
device nicknames, appearance/auto-lock and semantic preferences. They exclude
plaintext secrets, authentication sessions, passkey bindings, stored folder
permissions and downloaded model binaries. Search model installation must be
checked again after restoration. Passkeys must be enrolled again after complete
browser-data loss. No device identity or old delivery receipt is reused.

A validated staging database is activated through an account-specific browser
reference. Existing installations without a reference continue to open their
original database. Older application versions do not understand restored-store
references, so downgrading can show the retained original store.

## Evidence and remaining acceptance

The original Android incident was not reproduced. The user reported that opening
another device restored documents and sync settings, then confirmed offline file
opening on the Android PWA. This does not establish eviction as the cause.

Automated coverage uses real Chromium file handles from a test-only OPFS folder
behind the directory-picker boundary. That verifies encryption, write failures,
retention, restore and browser-store loss, not physical Android folder permission
behavior. The production picker requests an external user-selected directory;
OPFS is never the production backup fallback.

Verification on 2026-10-19:

- [x] Production web build, including TypeScript checking and service-worker generation.
- [x] 45/45 crypto checks and `git diff --check`.
- [x] Final focused Chromium run: 28/28 cases across offline backup, UI parity,
  and sync pause. This includes rapid preference changes before repaint, mobile
  widths 320/390/600, offline reload, folder recovery, retention, interrupted
  writes, quota/permission errors, multi-chunk authentication, revision history,
  pending work, deletion records, fresh restored identity, corruption repair,
  another tab's account lock, and delayed dialog-close focus handling.
- [x] Existing cross-client/browser integration cases: 11/11 passed in the earlier
  26-case run. Separate wider coverage also passed passkey, relay, sync-status,
  menu and selective-Trash cases. The wider run's focus failure was fixed and
  verified in the final focused run above.
- [ ] Physical Android external-directory selection, permission renewal and restore.
- [ ] Hosted rollout and installed-PWA update acceptance.

Reproduce the final focused run after building:

```sh
npm --prefix web run build
npm --prefix web test
PW_DISABLE_TS_ESM=1 npm --prefix web run test:e2e -- e2e/offline-backup.spec.ts e2e/parity.spec.ts e2e/sync-pause.spec.ts
```

Physical Android acceptance remains required: select an external folder, reopen
and reauthorize if needed, read files in airplane mode with peers closed, interrupt
a backup, and restore from the folder after clearing local document storage.
Hosted deployment and installed-PWA service-worker rollout are separate checks.
