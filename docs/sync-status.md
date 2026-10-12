# Sync activity and device receipts

Klefki shows what is moving, which device created an incoming change, whether
the server is reachable, and which linked devices have saved their copies.
Desktop, browser, and PWA use the same status labels and progress rules.

## Using the status display

The header status opens **Sync activity**. An active transfer also appears in
a compact strip above the library with its name, direction, byte and chunk
progress, current phase, retry countdown, and a Details button. On small screens,
the activity panel opens as a bottom sheet.

**Up to date with server** means this client completed its sync pass.
**Waiting for My phone** means the server has changes that device has not yet
acknowledged saving. These are separate from device presence: **Recently
connected** means a recent heartbeat reached the server, while **Not recently
seen** means live presence has expired or the server has restarted. It does not
assert a direct connection between devices. Status older than 45 seconds loses
its receipt certainty until refreshed.

Device nicknames are editable in the activity panel or Sync & devices settings.
They are local preferences scoped to the account and server. They are not sent
to the server or copied automatically to other clients. Document names remain
encrypted; transfer names and the latest 50 activity records are held only in
the current unlocked UI session and cleared on lock.

## Progress, retries, and automatic sync

Downloads report received bytes while streaming, then authenticate and commit
the complete revision. Upload bytes advance only after the server accepts each
existing 4 MiB chunk. A file can have all its bytes transferred while still
showing Saving changes; it becomes Saved after publication or durable local
commit. Existing chunks are reported as already saved rather than newly sent.
There is no estimated global percentage for an incoming queue of unknown size.

Speed and remaining time appear only after two completed network chunks over
at least five seconds. Retry resets those estimates and shows the attempt and
countdown. Ten seconds without transfer progress displays Taking longer than
usual. Timeout, unavailable network, unreachable server, expired sign-in, relay
capacity, and other sync errors have separate states. Pending edits survive
failure. Sync now is available once the active pass finishes.

Clients sync on unlock, after local edits, on reconnect, on foreground return,
and every 60 seconds while active. Local changes are debounced for one second;
requests arriving during a running pass are coalesced. Web/PWA periodic work
pauses while hidden or locked. Mobile background execution is not provided.

Desktop progress events bypass library reads and their work lock. Server device
status endpoints bypass the relay transfer lock and have a separate rate budget.
Presence is sent every 15 seconds when idle and at most every five seconds during
a transfer. Status polling runs every five seconds with the activity panel open
and every 30 seconds otherwise.

## Transport and compatibility

The encrypted envelopes, chunk size, immutable revision bodies, and existing
sync response shapes are unchanged. Additive SQLite tables and existing
IndexedDB state/revision records retain opaque attribution and local nicknames.
New local revisions record their creating stable store identity atomically;
downloads retain it when imported, and relay republishing preserves it.
Historical revisions without attribution show Source unknown.

| Endpoint | Authenticated, account-scoped contract |
| --- | --- |
| `POST /v1/sync/presence` | Stable `device`, `clientKind`, `phase`, optional opaque `revision` and `direction`, and `doneBytes`/`totalBytes`. No document names or nicknames. Device must already be registered and match `X-Klefki-Device`. Returns 204. |
| `GET /v1/sync/status` | `{epoch, latestSequence, devices}` with device type, reported phase/progress, last contact, durable acknowledged sequence, and outstanding account change count. |
| `GET /v1/sync/provenance?after=N` | `{changes}` of at most 16 records containing sequence, revision ID, original device when known, and latest publishing device. |

Revision publication optionally supplies `X-Klefki-Origin-Device`. The original
source is retained; the latest publisher can change on a later publication.
This is server-reported bookkeeping, not cryptographically authenticated
provenance. Receipt counts use actual account changes rather than sequence
subtraction, since sequence numbers can have gaps. A changed server epoch
replaces cached receipt state; live presence clears on restart.

Older clients continue using the existing sync wire shapes. A new client against
an older server continues syncing and displays Device status unavailable when
the additive endpoints are absent. To expose full device activity on a hosted
server, deploy the updated server and web build together. Existing installed
PWAs receive the changed assets through the existing service-worker update flow.

## Acceptance evidence — 2026-10-12

- [x] Full Go regression suite and `go vet ./...` passed.
- [x] Race checks for sync, server, and desktop passed, including durable
  receipts, account isolation, stale presence, lock-independent status,
  attribution retention, and failed publication preserving pending work.
- [x] Desktop production build and 34 frontend tests passed. Tests cover live
  progress while a library read is blocked, pending recipient delivery, local
  nickname saving, stale presence, and clearing private activity on lock.
- [x] Web production/PWA asset build and 43 crypto checks passed.
- [x] Eight existing Chromium sync/relay cases and two new sync-status cases
  passed. The new cases exercise real two-client transfers, automatic uploads,
  visible unacknowledged chunk progress, preserved keyboard focus, attribution,
  nickname persistence and absence from server responses, receipt status,
  older-server fallback, and offline pending work.
- [x] Six desktop browser layout/settings cases passed at widths down to 320px.
- [x] Native Linux executable rebuilt with the production frontend and ONNX tags.
- [x] Installed desktop executable updated in `~/.local/bin`; its bytes match
  the rebuilt binary. The prior executable is retained for rollback.
- [ ] Native-window network walkthrough, physical phone/PWA update acceptance,
  and deployment of these changes to the hosted server remain separate checks.
