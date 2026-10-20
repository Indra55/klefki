# Copy, move and download selected items

Choose **Select items** in a folder or the library root, select files and folders,
then choose **Copy to…**, **Move to…** or **Download ZIP**. Ctrl/Cmd-click starts selection;
Shift-click extends the range. Select all, deselect all and cancel are available.
Selection stays within one directory and clears when the browsing context changes
or the vault locks. Search results, Favorites and Trash do not offer bulk transfer.

The destination picker excludes the current directory, selected folders and their
descendants, Trash and conflicted folders. A move preserves document identities
and nested folder relationships. A copy includes every descendant and creates
independent objects, file keys and encrypted chunks. Editing or deleting a copy
does not alter the original. Names, tags, favorites and folder icons are retained.

Each batch commits all revisions and copied contents in one local transaction.
The incremental sync outbox is written in that transaction too. A changed library,
missing or corrupt copied content, cancellation, or an invalid destination leaves
no partial copy. Failed transfers retain the selection for review and retry.
Copies require local file contents; sync missing files before copying them.

**Download ZIP** exports the selection as one `klefki-documents.zip`, with all
descendants and empty folders retained. These are decrypted document exports,
like single-file Export. Duplicate names receive numbered suffixes, and unsafe
path components are normalized. A missing, damaged or conflicted file stops the
whole export; selection remains available for retry. Sync missing content first.

Desktop asks where to save, streams verified contents into a private temporary
archive, and publishes it only when complete. Existing destinations are never
overwritten. Cancel and lock discard unfinished output. Browser/PWA verifies one
file at a time, assembles a single downloadable Blob, and clears it if preparation
fails or the vault locks. ZIP data is stored without compression; browser memory
still limits large selections. ZIP64 supports entry counts beyond 65,535. No
additional dependencies or encryption-format changes are required.

## Acceptance, 2026-10-20

- [x] Core race tests cover mixed selection, recursive copy, independent encrypted
  contents, moves, invalid destinations, stale snapshots, missing/corrupt contents,
  cancellation, atomic rollback and encrypted sync.
- [x] Both web Chromium cases pass: recursive mixed copy/move, independent edits
  received by another browser, root/nested destinations, cancel, and corrupt-copy
  rollback with retained selection. Layout checks pass at 320, 390 and 1200px.
- [x] Desktop Chromium selection check passes with a fixture bridge, including
  Ctrl/Shift ranges, mixed selection, safe destinations and 320–1200px layouts.
- [x] The combined desktop frontend suite passes all 54 tests. Web, desktop
  frontend and Linux native production builds passed with both features present.
- [ ] Hosted rollout, installed native-window interaction and a physical PWA
  walkthrough remain separate acceptance checks.

## Bulk download acceptance, 2026-10-20

- [x] Desktop archive tests and race checks cover mixed selections, nested/empty
  folders, duplicate and unsafe names, unchanged source data, existing output
  preservation, corrupt contents, stale selections and cancellation cleanup.
- [x] All 55 desktop frontend tests pass, including canceled/failed save handling
  with selection retained. The desktop browser fixture verifies the selected IDs
  and Download ZIP visibility at 320–1200px.
- [x] Browser-generated archives open with an independent ZIP reader and retain
  exact contents, Unicode names, empty files, folder paths and duplicate names.
  A 65,536-entry ZIP64 archive passes the same reader's integrity check.
- [x] Browser missing/corrupt contents and lock cancellation publish no download.
- [x] Web, desktop frontend and ONNX-enabled Linux production builds pass.
- [ ] Hosted delivery, native save-dialog interaction and physical PWA acceptance
  remain separate from the automated checks.
