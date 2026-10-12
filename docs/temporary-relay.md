# Temporary relay and Render testing

Temporary relay keeps encrypted documents on the server until linked devices
have saved authenticated local copies. Persistent storage remains the default.
The setting applies to the whole account and preserves the existing encryption
formats. Update every client before enabling it.

## Enable it

Register the account once and connect each device to the same server.

- Desktop: **Settings → Sync & devices → Sync mode → Temporary relay**, then
  **Apply sync mode**.
- Web: open **Klefki settings** and enable **Temporary relay**.
- CLI: `klefki sync --server https://YOUR-SERVICE.onrender.com --sync-mode temporary-relay`.

Sync at least two separate local stores. Once every linked device acknowledges a
revision, the server clears its encrypted metadata and wrapped file key, and
removes document chunks when no undelivered revision or active upload needs them.
The first device alone never triggers cleanup. Local copies and revision history
remain on your devices.

An offline device holds deliveries in the queue. **Forget device** removes an
unused local store from that roster; it does not erase its downloaded data or
revoke login access. The forgotten store needs a fresh store to join again.
The CLI equivalent is `--forget-device DEVICE_ID`. Devices have stable opaque
IDs tied to their local stores, separate from expiring login sessions.

A new device joining after cleanup shows **Waiting for another device**. Sync
an existing device to supply its history again, then sync the new device. They
do not need to be online simultaneously. Switching back to **Persistent storage**
supplies missing history from your local store during subsequent sync passes.
It cannot restore records that no device retains.

Restoring an older backup makes the relay wait for that local store to catch up
again; sync another current device to supply newer history. When adding a device,
use the encrypted account file and a fresh document store. Running copies of the
same SQLite database on multiple devices duplicates their delivery identity and
is unsupported.

## Deploy a test instance on Render

The repository includes [render.yaml](../render.yaml) and
[Dockerfile.render](../Dockerfile.render). These build the server and PWA together
and run as an unprivileged user. The image build downloads and checksum-verifies
the pinned BGE-small assets so model installation works in the website and PWA.
The browser downloads about 139 MiB only when semantic processing is enabled;
inference runs on the user's device. These downloads count toward the service's
outbound bandwidth.

1. Put this revision in the Git repository you connect to Render.
2. Create a Render Blueprint from that repository using `render.yaml`. Alternatively,
   create a **Web Service**, select **Docker**, choose the **Free** instance, set
   the Dockerfile path to `./Dockerfile.render`, and health check to `/healthz`.
3. Open the service's HTTPS URL. Create or open your encrypted account and register
   it once. Enable temporary relay using the controls above.
4. Open the same encrypted account file on another device, use your password,
   and connect to the same HTTPS URL. Sync both devices and check that you can
   open the transferred document locally.

The container uses Render's `PORT`, stores its temporary queue under `/data`, and
serves the PWA at `/`. It uses explicit `-proxy-http`: the platform terminates TLS
and the API requires `X-Forwarded-Proto: https` on forwarded requests. `/healthz`
also accepts plain internal readiness checks. Use this flag only behind a proxy
that controls that header and restricts direct access to the origin listener.
Direct TLS and loopback development modes remain available.

Render free services sleep after inactivity and lose local filesystem changes
on restarts, redeploys, or spin-down. Accounts, sessions, cursors and queued
ciphertext can therefore disappear together. Existing temporary-relay clients
detect loss of their known account, register the same encrypted account again,
reset their cursors when the server epoch changes, and supply retained local
history. Restore requires the correct account file, password or supported local
unlock, and at least one surviving local copy. A brand-new client cannot restore
documents by itself. Known device rosters are merged after a reset; previously
forgotten devices can reappear if another store still remembers them.

Free hosting still has uptime and bandwidth limits. The queue is not a backup.
Keep [encrypted backups](backup-restore.md), particularly before clearing browser
storage or deleting a local database. See Render's current
[free-service limits](https://render.com/docs/free),
[Blueprint specification](https://render.com/docs/blueprint-spec), and
[TLS behavior](https://render.com/docs/tls).

## Storage and protocol limits

The default temporary queue allowance is **256 MiB per account** for encrypted
chunks plus queued encrypted metadata and wrapped keys. Set
`KLEFKI_RELAY_MAX_BYTES` in the Render service, or `-relay-max-bytes` when running
the server directly, to change it. HTTP 507 stops new uploads; local edits stay
intact. Clients can still receive queued revisions and acknowledge them to free
space. A document must fit inside the queue allowance with its encrypted
metadata. Interrupted uploads stay queued for retry and are pinned until their
revision is published; abandoned uploads currently have no expiry.

This allowance excludes database/filesystem overhead. The server retains account
unlock records, authentication verifiers, expiring session rows, device IDs,
cursors, revision IDs, links and ciphertext digests for idempotent retries and
late-device recovery. These small records grow with activity. Temporary relay
uses disk during delivery; it does not implement a storage-free server or promise
forensic erasure from host disks, snapshots or operator backups. Cleanup has no
timer: every linked device must acknowledge delivery. Enrollment is limited to
64 local stores per account.

| Endpoint | Contract |
| --- | --- |
| `GET /v1/sync/settings` | `{mode, epoch, queued}`; `queued` counts revisions still carrying ciphertext. |
| `PUT /v1/sync/settings` | `{mode: "persistent" \| "temporary-relay"}`; sets account-wide policy. |
| `POST /v1/sync/relay/device` | `{device, known:[...], sequence}`; registers a stable store, merges peers, and lowers a previous receipt if its durable local cursor is older; returns settings and `devices`. |
| `POST /v1/sync/relay/need` | `{ids:[...]}`, up to 16; returns missing payloads that this client should supply. |
| `DELETE /v1/sync/relay/devices/{id}` | Forgets a peer and releases its queue hold; the current local store cannot forget itself. |

Every endpoint requires the usual bearer session. Relay sync/blob requests also
send `X-Klefki-Device` for an enrolled local store. Older clients receive 428;
legacy metadata endpoints are unavailable in temporary mode. Purged change-log
entries retain identity/links and use `metadata: ""`. Clients use an authenticated
local copy or wait for a source, rather than treating a missing payload as an
empty library. HTTP 507 is not retried automatically within a request.

The server trusts authenticated clients' durable acknowledgements. A stolen
credential or malicious client can disrupt delivery or cause premature cleanup;
this mode adds no cryptographic proof of receipt, authenticated history, or new
protection against a malicious server.

## Verification record

Implementation and local verification: 2026-10-12. Core integration tests cover
two-device cleanup, a late join, offline peers, forgotten peers, exact revision
retry checks, returning to persistent storage, queue exhaustion and recovery
after deleting the entire server data directory. Real Chromium checks cover
offline reload, late-device recovery and Go/browser transfers after cleanup.
An older local snapshot is also tested against previously purged newer history.

Passed checks:

- `go test -race ./...` and `go vet ./...`.
- 43 web crypto checks and 30 desktop frontend tests.
- 29 Chromium cases: seven PWA cases, 21 passkey cases and one multi-browser
  relay case. The PWA cases include a Go/browser relay transfer.
- Production web and desktop frontend builds, and the native Linux desktop
  build with ONNX support at `desktop/build/bin/klefki-desktop`.
- `Dockerfile.render` image build and local container smoke checks for readiness,
  PWA serving, explicit HTTPS proxy behavior and UID 10001.

Live Render deployment and real-provider restart acceptance remain pending.

### Sync responsiveness fixes, 2026-10-12

File selection during browser sync previously returned without saving anything.
Local browser actions now run in order independently of network sync. Desktop
sync no longer holds the main work lock, and downloads stage authenticated
ciphertext before opening the local database transaction. Existing documents
remain readable and editable while a network transfer waits; incomplete
revisions and their cursors remain unpublished.

Desktop edits share one debounce timer, window focus no longer starts a transfer,
and automatic polls check for pending work, server changes, device enrollment and
unconfirmed receipts before replaying history. Already saved identical revisions
advance the cursor without appearing as new downloads. Browser chunk comparisons
avoid converting entire encrypted chunks into hexadecimal strings.

Validation passed: `go test -race ./...`, `go vet ./...`, 43 crypto checks,
34 desktop UI tests, both production frontend builds and the native Linux build.
Eleven Chromium cases covered Go/browser transfers, conflicts, interrupted
uploads, tamper rejection, relay cleanup, late enrollment, transfer status and
offline storage. A new case saves a text document and image while sync is stalled,
then reopens and reads the local text offline. Go regressions cover library reads,
exports and edits during stalled requests and downloads, plus idle polling.

The installed Linux executable was updated and matched the rebuilt executable's
checksum. Interactive native acceptance and the updated live Render/PWA build
remain pending; these local checks do not verify the user's hosted account.

## Reducing hosted request latency

Updated servers accept `POST /v1/sync/relay/prepare` with `device`, `known`, `sequence` and up to 256 revision `ids`; they return the existing relay state plus missing `ids`. Further history uses `/v1/sync/relay/need-batch`, with the same 256-ID limit. Both endpoints cap bodies at 16 KiB. Existing device/need endpoints keep their prior shapes and 16-ID need limit. Updated clients fall back after unsupported-endpoint 404/405 responses.

Sync checks retained revision IDs in order, loading payloads only when needed. Chunks use windows of four authenticated transfers; revision publication stays sequential, and durable cursor acknowledgement follows local commit. Account locks serialize quota, publication and cleanup while network body reads and response writes happen outside the locks.

The server was deployed before clients. See [measured results and exact revisions](benchmarks/2026-10-13-relay-optimization/README.md) for hosted timings, local stage comparisons, compatibility tests and unchanged production rate-limit control.
