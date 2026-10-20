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
