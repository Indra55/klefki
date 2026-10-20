# Incremental sync workers

Approved 2026-10-20: durable incremental work queues, independent metadata,
upload and download lanes, and low-priority optional Trash transfers. No SSE or
closed-app push service is included. Existing v1 encrypted records and conflict
semantics are retained.

## Decision

Local writes persist their work in the same transaction as the revision. Remote
metadata and content-demand cursors describe discovered work independently of
the delivery cursor. A failed file therefore cannot rediscover the whole feed or
prevent a newer file from being received. The server advertises this capability;
older selective-content servers retain the existing compatibility path.

The coordinator processes ordered metadata and deletion fences, runs upload and
download jobs independently, and shares a four-request content budget. Metadata
discovery continues during file transfers. New active documents precede edits,
due retries/history, and optional Trash contents. Transfer guards yield between
chunk windows for higher-priority work and policy changes. A retry stores its
attempt, error category and next eligible time; authentication and rate limits
also affect the account. Capacity failures defer uploads while downloads can
continue. A receipt is sent only after verified
durable storage. Database transactions never span network waits.

Normal checks compare small account counters. File-demand deltas contain opaque
file IDs; they do not inventory every document. Full reconciliation is reserved
for migration, relay enrollment/restored receipts, server epoch/mode changes,
content policy changes, and explicit repair. Metadata-only edits reuse file IDs;
identical metadata saves are no-ops.

New remote documents produce durable unread activity. Initial bootstrap is
quiet. Authenticated discovery shows receiving activity; verified local commit
refreshes the library and announces readiness once. Foreground clients show a
toast with Open; background running/unlocked clients can show OS notifications.
Browser permission is requested by an explicit Enable notifications action.
Names remain in encrypted records; unread bookkeeping stores opaque IDs. Lock
clears decrypted UI state. Browser suspension and auto-lock can delay receiving.

## Acceptance

- [x] Unchanged sync has no blob traffic, content inventory, or recreated jobs.
- [x] A new document passes an older failed transfer in both directions.
- [x] New jobs and incoming metadata progress during a stalled transfer.
- [x] Upload/download lanes share at most four content requests.
- [x] Metadata-only and identical edits avoid content transfers/new revisions.
- [x] Retry state survives restart without spinning or losing work.
- [x] Relay cleanup, late join, restored stores, deletion fences, conflicts,
      cancellation, corruption and optional Trash policies remain correct.
- [x] Active receivers update without reload/manual sync; ready documents appear
      while other jobs continue.
- [x] Toasts/unread activity are deduplicated.
- [x] OS permission denial leaves incoming document access working.
- [x] Core/server, browser/PWA and desktop frontend checks pass.
- [x] Before/after diagnostic records request counts and blocked-document behavior.

## Validation, 2026-10-20

- `go test ./... -timeout=300s` passed; the affected core sync, server and desktop
  packages passed again after the final cursor/index changes. Worker/feed race
  checks and `go vet ./...` passed. A separate failed-upload regression passed
  with the race detector.
- All 54 desktop frontend tests and 45 web crypto checks passed. Web and desktop
  production assets built, and the ONNX-enabled Linux executable was rebuilt at
  `desktop/build/bin/klefki-desktop`.
- All 32 distinct Chromium cases passed across the regression run and focused
  reruns: offline backup/recovery, authenticated corruption repair, relay
  recovery/batching/legacy capability fallback, pause/resume, progress/receipts,
  incremental workers, notifications and IndexedDB v1 migration. Tests now wait
  for the selected file's readiness because other files can still be downloading.

The same synthetic in-process diagnostic was run against the unmodified
`98d001c` baseline and this working tree. Four existing documents, an injected
503 on an older file, and a later `scan_new.txt` were used:

| Scenario | Before | After |
| --- | --- | --- |
| Unchanged persistent pass, file IDs checked | 4 | 0 |
| Unchanged relay pass, file IDs checked | 6 | 0 |
| Pass with older failure and `scan_new`, file IDs checked | 6 | 1 |
| `scan_new` available after that pass | No | Yes |
| Blob requests in that pass | 2 failed old-file attempts | 1 successful new-file download |

Both versions already transferred zero blobs on an unchanged pass. The measured
improvement removes repeated file-state work and isolates failures; these counts
are not an end-to-end speed multiplier or a physical-device benchmark. A pass
can still report waiting for the old file after the new document is available.

## Browser/PWA refresh correction, 2026-10-20

Overlapping library refreshes could finish out of order: an older IndexedDB read
could hide a document already downloaded and announced by a newer refresh. Idle
sync then had no remaining work to trigger another render. Only the latest
refresh may now publish its results. A controlled stale-read browser regression
failed before this correction and passed afterward, including opening the
received file without reloading or manually syncing.

Switching away also canceled the one-second upload debounce. Visibility changes
now check for work immediately, local writes made while hidden bypass that
debounce, and a check arriving during a local operation remains queued. A second
regression delays the upload debounce and five-second source polling timer by
ten minutes: switching away still starts the upload within five seconds and the
other browser receives it automatically. This test failed before the correction.
It simulates delayed timers; it does not prove execution after an OS suspends a
physical phone.

The production web build passed. All 11 Chromium cases across `sync-workers`,
`sync-pause` and `relay` passed across the regression run and focused rerun.
Coverage includes automatic relay folder creation, document arrival, rename and
move, paused sync, failed older transfers, idle no-op checks and late-join relay
receipts. Relay preview assertions now wait for that file's local contents, and
cleanup assertions wait for delivery receipts rather than metadata visibility.

## Additive protocol and rollout

### Browser retry isolation

The browser coordinator runs library discovery, local revision publication,
content jobs and delivery receipts independently. Two publication jobs preserve
each object's revision ancestry; an unrelated publication may proceed when an
older revision fails. Two content jobs still share four blob requests. Planning
belongs to each content job, so discovery or a different file's preflight cannot
hold the content scheduler. Queued metadata alone no longer interrupts every
active file. New active documents retain priority over edits, retries and Trash.
An idle transfer direction can borrow the second content slot; equal-priority
work in the opposite direction reclaims that slot rather than waiting behind
two stalled transfers.

Publication failures persist their retry deadline on the encrypted revision's
local bookkeeping. Discovery and receipt deadlines persist separately in state.
Each worker makes one network attempt and yields on failure; there is no sleeping
retry inside a worker slot. Lock cancels and joins every active job. Metadata
receipts wait until discovered content requests have been registered, preserving
relay cleanup fences. Unchanged cursor acknowledgements are deduplicated, and a
successful publication batch catches the metadata cursor up before reporting
completion.

Server auth, status, library-control and document-data request budgets are
separate, each retaining the configured burst and refill. Library feed checks,
deletion synchronization and cursor acknowledgements use the library budget.
`429` responses identify their budget with `X-Klefki-Retry-Scope`. Updated web
clients honor a library cooldown without delaying publication or blob requests;
unscoped data limits retain the shared document cooldown for compatibility.
Status throttling and an individual request's `503 Retry-After` cannot impose a
cooldown on document data. HTTP failure status and the affected job/document are
visible in activity. A document-data limit still defers document requests.

Validation: both new blocking regressions failed against the preceding build.
After correction, the receiving browser verified a new upload while the source's
library request remained held, and another new document passed an older
publication's 30-second retry without retrying that publication early. Its
deadline and dependent-revision ordering survived reopening. Another test held
two downloads and verified an upload in the opposite direction before releasing
them. Separate browser cases cover status, scoped library and unscoped data
limits; the server test exhausts the library bucket without consuming the
document bucket.

All 29 distinct Chromium cases passed across the regression run and focused
reruns, including conflicts, corrupt downloads without false receipts, relay
bootstrap/cleanup, pause, Trash policy and expiry. The interrupted-upload case
checks that encrypted chunks survive reopening and then upload successfully.
The expiry fixture now creates the same revision/work bookkeeping as a local
edit. `go test ./server ./core/sync -timeout=300s`, the final server rerun, all
55 desktop frontend tests, and the production web build passed. Hosted
authenticated accounts and physical PWA background execution were not exercised.

`GET /v1/sync/settings` advertises `work_queue`. The new endpoints require the
existing authenticated account session and enrolled `X-Klefki-Device`:

| Endpoint | Purpose |
| --- | --- |
| `GET /v1/sync/check` | Epoch/mode and metadata, content, deletion, policy and enrollment counters. |
| `GET /v1/sync/content/changes?after=N&through=M` | Captured, account-scoped content-demand/availability deltas, at most 256 opaque file IDs per page; own-device signals are excluded. |
| `PUT /v1/sync/metadata-cursor` | Acknowledge authenticated metadata after all wanted content requests are registered. Outstanding requests continue pinning their file contents. |

Enrollment adds optional `metadataSequence`; legacy devices keep their existing
delivery-based cleanup boundary. Deploy the server and web assets together,
then update native clients. Updated clients fall back to the existing selective
path when the server lacks `work_queue`. Local SQLite/IndexedDB migrations retain
the encrypted revision and chunk formats. No dependencies or key hierarchy change.

Hosted deployment, installed native-window interaction, physical phone/PWA
background delivery and OS notification presentation require separate acceptance.
The rebuilt Linux executable has not been installed or launched in this run.
Linux system notifications require `notify-send`; other native platforms expose
the toast and unread list. Native minimize-lock behavior is retained. Browser
background execution is best effort, and no notification arrives while locked
or after the application is closed.
