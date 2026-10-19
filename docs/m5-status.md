# M5 web client

Implemented in `web/`: TypeScript/libsodium-sumo crypto, an installable PWA shell,
IndexedDB ciphertext storage, and the existing M3 revision/chunk protocol.
The approved v1 formats and key hierarchy are unchanged. The crypto dependency
remains the approved `libsodium-wrappers-sumo` / `libsodium-sumo` 0.7.15 pair.

## Run

Requires Go 1.26+ and Node 22.18+ (Node 26.9 used for verification).

```sh
npm --prefix web ci --ignore-scripts
npm --prefix web run build
go run ./cmd/klefki-server -dev-http -web web/dist
```

Open `http://127.0.0.1:8080`. Loopback HTTP is development-only. For normal use,
serve the same build using the existing `-tls-cert`, `-tls-key`, and `-listen`
options, and open that HTTPS origin. The PWA uses its own origin for all API calls;
no CORS configuration or external assets are required. The optional `-web` flag
must point only to the public build directory, never a data directory.
The existing Docker image remains API-only; build and mount `web/dist` and pass
`-web` explicitly if using that image to serve the PWA.

For frontend development, run the API on port 8080 and `npm --prefix web run dev`.
Vite proxies `/v1` to the loopback API. Offline installation requires the production
build above; the development server does not generate a service worker.

## Use with desktop

1. On desktop, export the encrypted account file and sync the documents to the
   server. On web, select **Open an encrypted account file**, or enter the account
   ID to retrieve its wrapped records from the same server.
2. Unlock with the existing password, open the sync status menu in the top bar,
   and select **Sync now**. Open or export the
   desktop document. This does not upload your password or master key.
3. Add a document on web and select **Sync now**, then sync desktop and open it.
4. After the web app has loaded online and installed its worker, disconnect.
   Upload files or use a document's **Actions → Edit details & move** to change
   names, tags, favorites, folders, or text content
   for supported text files up to 1 MiB. Reload, unlock, and check the local edit.
5. Reconnect. An online event starts sync while unlocked; **Sync now** also works.
   Sync desktop to see the edit. Concurrent edits remain explicit conflict copies.

New web accounts display a recovery code once. Save it separately, download the
wrapped account file from **Settings → Sync & devices**, then register the
account once using **Register this store on server**. Registration is explicit; account creation and
local edits work offline. Browser recovery-code unlock/reset is not implemented;
the exported account is compatible with the existing local recovery tools.

## Desktop design on web and PWA

Desktop and web use the same `ui/style.css` and `ui/categories.ts`. The web library
has the dark desktop layout, all six built-in category tabs plus synced custom
tags, name/type/size sorting, favorites, folder breadcrumbs, and reversible trash.
**All documents** includes files inside folders; **Folders** browses one level.
Document actions and previews are available from the document row.

**Settings** opens a dedicated page with Appearance, Privacy & lock, Sync &
devices, Offline & backup, and Search. Theme, spacing, default sort, and inactivity timeout are
saved on this browser. Passkey controls remain browser-specific. OCR/index
building and automatic file organization run on desktop; sync brings the index
and reviewed tags to web. Browser local search and suggestions remain optional.

Text, images, and PDFs preview after local authenticated decryption. PDF page
navigation and zoom use the same PDF.js version as desktop and work offline.
Unsupported formats offer export. Lock closes previews and clears their bytes.
The PWA caches the PDF worker and Inter fonts with its shell; API responses and
private plaintext are never added to that cache.

### Share documents

Unlock Klefki, then choose a document's **Actions → Share**, or **Share** in its
preview. Tap **Choose app** to open the system share sheet. Any compatible app
offered by the operating system can receive the file; Klefki has no app allowlist.
The file retains its name and bytes, with a MIME type inferred from its extension.
Unknown extensions use `application/octet-stream`. Sharing uses a locally
decrypted copy, without creating a public link or uploading plaintext to Klefki's
server, and works offline for locally available documents.

The share sheet requires a supported browser on HTTPS (loopback is suitable for
development). Klefki checks file-sharing support for the actual file. Unsupported
files or browsers offer **Export** instead. Cancellation and sharing errors allow
retry; they never automatically download the document. Preparing the file before
the **Choose app** click preserves the browser's required user activation.

Closing the dialog or locking drops the prepared file reference. Row-action
decryption buffers are wiped after preparation; previews retain theirs until
closed or locked. Browser-managed `File` copies cannot be reliably wiped.
Lock cannot withdraw a copy already handed to the
operating system or another app. This feature starts in Klefki; appearing inside
another app's attachment picker still requires native integration.

Verification on 2026-10-15: the production web build, six sharing browser tests,
and seven menu/PWA parity regressions passed. Sharing tests exercise real encrypted
imports and decryption, file names/types/bytes, offline preview sharing, retry,
explicit export fallback, and lock cleanup. `navigator.share` and `canShare` are
stubbed in Chromium; physical phone share sheets and receiving apps remain
unverified. See the [Web Share API guide](https://web.dev/articles/web-share)
for browser capability requirements.

After a deployment, existing browser tabs and installed PWA windows may keep the
previous worker until all of them close. Close and reopen them once to pick up
this UI. Subsequent updates show **Update and reopen**, which locks before
activating the new worker and reloading. Account storage and ciphertext are kept;
do not clear site data to refresh the UI.

Verification on 2026-10-12: web and desktop frontend builds, a native Linux build,
43 web crypto checks, 30 desktop frontend tests, and 34 browser tests passed.
Browser checks cover encrypted desktop/browser transfers, conflicts, relay
cleanup, passkeys, categories, folders/favorites/trash, saved preferences, mobile
layout, offline PDF pages/zoom, lock cleanup, and update/reload persistence.
Three optional M8 browser tests were skipped because a verified model bundle was
not available. The server race suite passed. The Render image built and served
its health endpoint, matching UI bundle, Inter font, and PDF worker successfully.

## Storage and locking

Only wrapped account records, encrypted passkey unlock wrappers, encrypted
revisions/chunks, opaque IDs, queue order,
origin binding, cursor, and browser appearance/lock preferences are persisted.
Local storage remembers the wrapped
account; IndexedDB holds the offline klefki. The worker caches only enumerated
build assets, never `/v1`, plaintext downloads, credentials or account responses.
An edit and its queue entry commit together. Authenticated metadata may arrive
before file contents; incomplete documents stay visible. Required content receipts
and delivery progress follow authenticated local chunk saves. Retries reuse existing ciphertext and
skip already-uploaded chunks. Partial downloads are retried at revision granularity.

Active unlock keys and bearer sessions stay in memory. Optional
[passkey unlock](passkey-web.md) persists a PRF-protected encrypted wrapper for
the document master key and server login credential, separately from synced
account records. Manual lock, page exit,
and the selected inactivity timeout (five minutes by default) clear active keys,
previews and object URLs;
ongoing network requests are canceled. Web Locks prevent two tabs from opening
the same local account concurrently. Session tokens discarded on lock expire
server-side normally; browser lock does not revoke the server device session.
JavaScript strings, browser downloads and runtime copies cannot be reliably wiped.

## Verification (2026-10-10)

- `npm --prefix web test`: **43/43** crypto checks passed, using production crypto
  functions: all shared KDF/record/stream/negative vectors, strict metadata/Unicode,
  recovery encoding, account containers and wrong-password rejection.
- `npm --prefix web run build`: TypeScript and production build passed. The
  libsodium browser bundle is about 1.1 MB uncompressed; Vite reports a size warning
  and externalizes the library's unused Node `crypto` import. Browser crypto works.
- Chromium acceptance against a real Go server: desktop service imports a
  multi-chunk binary, text and empty file; browser authenticates/downloads the
  binary byte-for-byte; a web upload reaches the desktop service's preview;
  an offline text edit survives offline reload and syncs to Go.
- Browser failure cases passed: two offline edits preserve both heads; an upload
  interrupted before revision publication survives reload/retry; tampered chunks
  are not saved and do not advance the delivery cursor; authenticated document
  metadata remains visible. Another tab is excluded until lock.
- Web signup/registration, metadata-only editing, and five-minute automatic lock
  also passed. **Six browser acceptance cases passed** (five integration cases plus
  the signup/auto-lock case).
- Browser persistence inspection found none of the synthetic filenames, password
  or document text in stored records. This is a targeted assertion, not an audit.
- `go test ./...`: passed, including existing crypto, CLI, desktop, sync and server
  suites and the new static-hosting tests.
- `go test -race ./server ./core/desktop ./core/sync ./web/tests/bridge`: passed.
- Patched Vite 7.3.7 and Playwright 1.64.0 were installed; npm reported zero known
  dependency vulnerabilities at installation time.

Reproduce browser checks with `web/node_modules/.bin/playwright install chromium`
and `npm --prefix web run test:e2e`. The harness creates temporary synthetic
accounts/server data and removes them on normal exit. It exercises the built
PWA; build first. It does not expose test-only HTTP endpoints.

## Limits

The cross-client proof exercises the real desktop Go import/preview service and
sync store plus Chromium, not a new manual Wails UI walkthrough on both OSes.
Firefox, Safari, mobile browsers, OS-level PWA installation, deployed HTTPS and
power-loss durability were not tested. Browser quota/eviction can remove local
copies. Requesting persistence protects against automatic eviction when granted;
explicit clearing remains possible. Keep an independent encrypted backup using
the PWA folder workflow or the desktop/CLI M6 workflow. A compromised origin
can deliver malicious JavaScript, as covered by the existing threat model.

Uploads, downloads and revision imports buffer a complete file's ciphertext;
very large documents can exceed browser memory or quota. Argon2id runs on the
main thread and can briefly pause the UI. Browser OCR, recovery-code unlock,
and background sync while locked are not included. PDF preview and Trash are
available as described above. Folder backups update only while unlocked.
No deployment or Git commit was performed.

## Offline storage and folder recovery

See [PWA offline storage and folder backup](pwa-offline-backup.md) for browser
storage persistence, missing-content states, automatic encrypted folder copies,
and verified restoration without a server or another device. Physical Android
folder permission and hosted deployment acceptance remain separate from browser
regressions.
