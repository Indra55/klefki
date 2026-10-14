# Client fixes

Requested 2026-10-14. Work through these in order, finishing and validating one
issue before moving to the next. Commit and push each validated fix before
starting the next issue; closely related fixes may share a commit. Existing
unrelated documentation work stays intact.

| # | Task | Status |
|---|---|---|
| 1 | Keep document editing open during background refresh on desktop, web and PWA | Fixed and validated |
| 2 | Add consistent folder icons using the requested iOS emoji style | Queued |
| 3 | Add a per-device sync pause/resume control for bulk local uploads | Queued |
| 4 | Keep mobile three-dot menus fully inside the viewport | Queued |
| 5 | Make passkey unlock available on supported web browsers too | Queued |
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

## Acceptance for queued tasks

2. Research a usable source and its redistribution terms before bundling the
   requested emoji artwork. Folder icons should look consistent across clients.
3. Pause automatic transfers on that device while keeping local imports/edits
   queued; make the paused state visible and resume explicitly. Pausing cannot
   prevent conflicts from concurrent edits on different devices.
4. Verify menus near each viewport edge at narrow mobile widths.
5. Inspect the existing PRF/WebAuthn implementation and browser eligibility;
   keep password and recovery unlock available.
6. Use original artwork with a key/keyring motif, then update desktop and PWA
   icons, including install and maskable variants.
7. Inspect the current sync failure before resetting anything. Offer a clear,
   explicit local replacement flow that preserves account access and obtains a
   complete authenticated copy. Temporary relay may require another current
   device online because delivered ciphertext can be removed from the server;
   do not assume the server contains a full backup or select a source by its
   timestamp alone.
