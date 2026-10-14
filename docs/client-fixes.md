# Client fixes

Requested 2026-10-14. Work through these in order, finishing and validating one
issue before moving to the next. Commit and push each validated fix before
starting the next issue; closely related fixes may share a commit. Existing
unrelated documentation work stays intact.

| # | Task | Status |
|---|---|---|
| 1 | Keep document editing open during background refresh on desktop, web and PWA | Fixed and validated |
| 2 | Choose an iOS-style emoji per folder, synced across clients | Fixed and validated |
| 3 | Add a per-device sync pause/resume control for bulk local uploads | Fixed and validated |
| 4 | Keep mobile three-dot menus fully inside the viewport | Fixed and validated |
| 5 | Make passkey unlock available on supported web browsers too | Fixed and validated |
| 6 | Replace the app icon with an original minimalist key-inspired design | Queued |
| 7 | Diagnose stale Linux desktop sync and add an explicit fresh-download option | Queued |

## 1. Document editor closes by itself

- Web/PWA: `shell()` now keeps active dialogs attached during library refresh,
  preserving draft fields and focus. Lock still removes all private dialogs.
- Desktop: library refresh now preserves the editor when its revision leaves
  the current head list. The existing stale-save check reports a changed
  document without discarding the draft.
- Both regression tests reproduced the original dismissal before the fix.
- Validation: 41 desktop UI tests and 11 Chromium tests passed, including editor
  drafts at 1280px and 390px, text/metadata save, cancel, focus, stale desktop
  saves, lock cleanup, library parity, offline/PWA update and sync activity.
  Web production build and `make linux-build` passed; the installed
  `~/.local/bin/klefki-desktop` matches the rebuilt executable byte for byte.
- Delivery: restart the desktop app to load its new executable. Hosted rollout
  and a physical installed-PWA walkthrough are separate from these local checks.

## 2. Per-folder emoji icons

- Added 32 locally bundled Apple emoji choices to folder creation and details
  across desktop, web and PWA, with keyboard selection and default-folder reset.
  Library rows and breadcrumbs show the chosen icon consistently.
- The user approved optional encrypted `folder_icon` metadata. Shared Go/TS
  fixtures check valid values and reject URLs, invalid scalars and file icons.
  Rename, favorites, moves and trash/restore preserve the selection; choices
  travel through the existing encrypted revision sync.
- Bundled artwork is pinned and credited separately from the application
  license; see [folder icons](folder-icons.md) and [artwork notice](../ui/emoji/README.md).
  Small PNGs are emitted as local files to work under the existing web CSP.
- Validation: `go test -race ./...`, `go vet ./...`, desktop-module tests and
  45 web crypto checks passed. All 41 existing desktop UI tests and the new
  picker regression passed (the new test was rerun after fixing its selector).
  Eight web editor/parity cases, the new cross-client/offline icon case, and
  the desktop frontend browser picker case passed. All 32 images loaded in
  both built frontends; keyboard/Escape and 320/390/600px layouts were checked.
- Web production and Linux native builds passed. The final Linux build is
  installed; restart the app to load it. Hosted rollout, a native-window
  walkthrough and physical installed-PWA testing remain separate checks.
- Older Go clients can drop this optional field when rewriting a folder;
  update linked clients before editing folder icons.

## 3. Pause and resume syncing per device

- Added Pause sync / Resume sync to Sync activity and Sync & devices on desktop,
  web and PWA, with a visible paused banner. Local files, folders and edits stay
  available and queued. Incoming changes wait too; other devices keep syncing.
- The local preference survives lock/unlock and restart. All automatic sync
  entry points and status/presence polling respect it. An active cycle finishes
  with an explicit pausing label; queued follow-up runs are cleared. Resume
  starts a sync without changing account-wide storage mode or encrypted formats.
- Pausing batches transfers; concurrent edits can still create conflict copies.
  See [sync activity](sync-status.md#pause-syncing-for-a-batch).
- Validation: desktop backend race suite, desktop vet and native module checks
  passed. All 43 desktop UI tests passed; the eight sync UI cases were rerun
  after final status-copy changes. Three new browser cases verify zero automatic
  API requests while paused, local imports/edits surviving reload, reconnect and
  polling triggers, active-cycle completion, and resumed file contents in both
  directions with persistent storage and temporary relay. All six existing
  relay/activity cases passed in the broader run; the new cases passed in their
  final focused rerun after tightening navigation and document selectors.
- Desktop frontend browser controls/layout passed at 320/390/600/800/1200px;
  web settings fit 320/390/600px. Web production and Linux native builds passed.
  The rebuilt Linux executable is installed and matches byte for byte; restart
  Klefki to use it. Hosted rollout, native-window network testing and a physical
  installed-PWA walkthrough remain separate checks.

## 4–5. Viewport-safe menus and desktop web passkeys

- Web/PWA action and View menus now measure the visible viewport, flip above
  their trigger when necessary, clamp to a 12px edge margin, and scroll if the
  screen is too short. Position updates on page scroll, resize and visual
  viewport changes, including zoom. Arrow keys, Escape and focus return work.
  Desktop menus retain Radix collision handling with the same edge margin and
  a scrollable height limit.
- Passkey setup no longer requires a built-in platform authenticator or forces
  an internal transport. Regular browser tabs and PWAs can use compatible PRF
  providers/security keys. Required user verification, encrypted local wrappers,
  origin/account binding and password fallback are retained. Existing recovery
  access through desktop/CLI is unchanged; no new browser recovery flow is added.
- Both regressions reproduced before the fixes. Validation: 22 Chromium passkey
  cases passed, including a virtual USB/PRF authenticator with the platform
  availability probe returning false, offline decryption, tampering, cancellation
  and missing WebAuthn. The menu browser case passed at 320/390/600/1280px, near
  upper/lower edges, after resize to 568x180, during scroll and at 2x visual
  viewport zoom. All 17 existing library/PWA/parity cases passed.
- All 43 desktop UI tests and the desktop browser menu edge/scroll case passed.
  Both production builds, 45 shared crypto checks and the native Linux build
  passed. The installed executable matches the rebuilt binary byte for byte;
  restart Klefki to load it. Hosted rollout, physical passkey providers/security
  keys, native-window interaction and installed-phone PWA acceptance remain
  separate checks. See [passkey setup](passkey-web.md).

## Acceptance for queued tasks

6. Use original artwork with a key/keyring motif, then update desktop and PWA
   icons, including install and maskable variants.
7. Inspect the current sync failure before resetting anything. Offer a clear,
   explicit local replacement flow that preserves account access and obtains a
   complete authenticated copy. Temporary relay may require another current
   device online because delivered ciphertext can be removed from the server;
   do not assume the server contains a full backup or select a source by its
   timestamp alone.
