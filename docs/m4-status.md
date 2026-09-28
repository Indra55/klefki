# M4 desktop status

The Wails desktop app provides vault creation/password and recovery unlock,
folders, tags, favorites, import/export, PDF/image/text preview, trash/restore,
sync status, manual lock, and automatic lock. Linux additionally provides optional
fingerprint unlock, automatic listening, contact-driven animation, and themed
success feedback.

M4 is accepted. The native manual walkthrough and current results are in
[M4 acceptance](m4-acceptance.md). The user reported successful Windows desktop
testing and explicitly confirmed the complete Linux walkthrough on 2026-10-10.
Real Linux fingerprint unlock, contact-only animation, and restart were also
confirmed by the user. These confirmations satisfy the milestone's native
acceptance gate; they are not agent-run native tests.

## Automated validation, 2026-10-10

- Desktop service tests passed under the race detector.
- Desktop service Go vet passed.
- Twelve frontend interaction tests passed.
- TypeScript/Vite production frontend build passed.
- Linux Wails production build passed with `desktop,production,webkit2_41` tags.
- Windows amd64 Wails production cross-build passed with `desktop,production` tags.
- Linux fingerprint signal-shape and contact-property tests passed.
- Chromium fixture layout/theme/search/lock check passed: one Playwright test.
  The fixture returns fresh state snapshots to match Wails serialization.
  On this host's Node 26, run it from `desktop/frontend` with
  `PW_DISABLE_TS_ESM=1 node node_modules/@playwright/test/cli.js test`; the installed
  Playwright loader otherwise stalls before starting tests.

## Evidence boundaries

Native Windows and physical fingerprint acceptance are user-reported. Automated
frontend/browser checks use fixture bridges; they do not exercise Wails native
dialogs or prove cross-device sync through the desktop UI. Desktop service tests
exercise the real local service with generated data and a fake fingerprint/keyring
provider. Commit status is separate from milestone acceptance.
