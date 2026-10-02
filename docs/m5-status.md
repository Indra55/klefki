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
go run ./cmd/vault-server -dev-http -web web/dist
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
2. Unlock with the existing password and select **Sync now**. Open or download the
   desktop document. This does not upload your password or master key.
3. Add a document on web and select **Sync now**, then sync desktop and open it.
4. After the web app has loaded online and installed its worker, disconnect.
   Upload files or use **Edit** to change names, tags, favorites, or text content
   for supported text files up to 1 MiB. Reload, unlock, and check the local edit.
5. Reconnect. An online event starts sync while unlocked; **Sync now** also works.
   Sync desktop to see the edit. Concurrent edits remain explicit conflict copies.

New web accounts display a recovery code once. Save it separately, download the
wrapped account file from **Vault settings**, then register the account once using
**Register this vault on server**. Registration is explicit; account creation and
local edits work offline. Browser recovery-code unlock/reset is not implemented;
the exported account is compatible with the existing local recovery tools.

## Storage and locking

Only wrapped account records, encrypted revisions/chunks, opaque IDs, queue order,
origin binding and cursor are persisted. Local storage remembers the wrapped
account; IndexedDB holds the offline vault. The worker caches only enumerated
build assets, never `/v1`, plaintext downloads, credentials or account responses.
An edit and its queue entry commit together. A pulled revision and cursor commit
only after authenticating every chunk. Retries reuse existing ciphertext and
skip already-uploaded chunks. Partial downloads are retried at revision granularity.

Password-derived keys and bearer sessions stay in memory. Manual lock, page exit,
and five minutes of inactivity clear active keys, previews and object URLs;
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
  do not commit a revision or move the cursor; another tab is excluded until lock.
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
copies; sync and the planned M6 backups remain necessary. A compromised origin
can deliver malicious JavaScript, as covered by the existing threat model.

Uploads, downloads and revision imports buffer a complete file's ciphertext;
very large documents can exceed browser memory or quota. Argon2id runs on the
main thread and can briefly pause the UI. No browser OCR, backup/export archive,
PDF renderer, trash management, remote account recovery or background sync while
locked is included. Desktop trash entries are hidden in the web listing.
No deployment or Git commit was performed.
