# Copy and move selected items

Choose **Select items** in a folder or the library root, select files and folders,
then choose **Copy to…** or **Move to…**. Ctrl/Cmd-click starts selection;
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
