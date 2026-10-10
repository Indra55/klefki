# M4 desktop acceptance

Run this walkthrough in the native Klefki desktop app on Linux and Windows.
Use a disposable klefki and generated documents. Record any failed step before
marking M4 complete. Browser fixtures and cross-compilation do not replace this
walkthrough.

## Preparation

- Build the frontend with `npm run build` in `desktop/frontend`.
- Linux: build in `desktop` with
  `go build -tags desktop,production,webkit2_41 -ldflags '-s -w' -o build/bin/klefki-desktop .`.
- Windows: build in `desktop` with
  `go build -tags desktop,production -ldflags '-s -w -H windowsgui' -o build/bin/klefki-desktop.exe .`.
  A Windows machine needs WebView2. The Linux build uses GTK and WebKitGTK 4.1.
- Prepare a small UTF-8 text file, a PNG/JPEG, and a PDF with two pages.
- Use a test server for the sync steps; the desktop connection requires HTTPS.

## Walkthrough

Repeat steps 1–11 on both operating systems.

1. **Create and unlock.** Create a klefki, save its recovery code outside the klefki,
   and acknowledge the save. Unlock with its password. Close and reopen the app;
   documents remain hidden until unlock. A wrong password keeps the klefki locked.
2. **Import.** Add the text, image, and PDF using the native file picker. Drop
   another generated file onto the window. Confirm each appears once and transfers
   finish. Cancel a separate import and confirm completed files remain available.
3. **Organize.** Create a folder, move a document into it, rename it, and add tags.
   Browse the folder and tag, search by name/tag, and change the sort order.
   Favorite a document and confirm it appears in Favorites.
4. **Preview.** Open each of the text, image, and PDF files. Verify their contents,
   image zoom, PDF page navigation, and PDF zoom. An unsupported file offers export.
5. **Export.** Export a document to a new path and compare it with the source.
   Try an existing destination; it must not overwrite that file.
6. **Trash and restore.** Trash a document, confirm it disappears from the library
   and appears in Trash, then restore it. Repeat for a folder containing a document.
   Check that contents remain available after restoring.
7. **Sync and offline.** Connect to the HTTPS test server in Settings and register
   the test klefki once. Confirm the sync indicator settles after upload. Set up a
   second test device using the encrypted account file and confirm download and
   preview work. Disconnect, add/edit a document, restart, and confirm it remains
   available locally. Reconnect and verify the queued change reaches the other device.
8. **Manual lock.** Lock using the toolbar and Ctrl Shift L. Names, previews, and
   pending private UI disappear. Unlock and verify saved documents return.
9. **Automatic lock.** Select a one-minute inactivity interval. Wait without
   interacting and confirm the klefki locks. Unlock, minimize, restore the window,
   and confirm it is locked.
10. **Recovery.** Lock and unlock with the saved recovery code. Local documents
    open; sync and fingerprint enablement remain unavailable until password unlock.
11. **Appearance and feedback.** Check dark, light, and system themes, a small
    desktop window, and keyboard navigation. A successful password unlock shows
    the themed toast briefly; failed unlocks do not show a success toast.
12. **Linux fingerprint extension.** Follow [the fingerprint checks](fingerprint-linux.md#verification-and-manual-checks).
    The icon is static while waiting and on hover/focus. Finger contact starts its
    animation; removal stops it. Verify automatic unlock, password fallback,
    cancellation, restart, disablement, and the fingerprint success toast.

## Results

| Evidence | Result | Source |
|---|---|---|
| Windows native desktop testing | Passed, user-reported | User reports already tested on Windows, 2026-10-10 |
| Linux physical fingerprint unlock, contact-only animation, and restart | Passed, user-reported | User confirmed all three in this session, 2026-10-10 |
| Full Linux steps 1–11 | Passed, user-reported | User explicitly confirmed all listed flows, 2026-10-10 |
| Desktop service race tests | Passed | `go test -race ./core/desktop`, 2026-10-10 |
| Frontend interaction tests | 12 passed | `npm test`, 2026-10-10 |
| Frontend build and Linux/Windows amd64 builds | Passed | Final validation recorded in `m4-status.md` |
| Chromium layout/theme/search/lock fixture test | Passed | One Playwright test, 2026-10-10 |

Only user-reported rows describe native manual testing. The agent's automated
checks use generated fixtures and do not establish physical fingerprint matches
or native Windows execution.
