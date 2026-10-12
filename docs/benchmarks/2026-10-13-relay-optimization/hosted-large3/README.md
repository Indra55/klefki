# Relay benchmark

Relay: https://klefki.onrender.com. Mode: hosted. Started: 2026-10-12T20:15:34.392Z.

4/4 received files passed SHA-256 verification. 0 synthetic initial folder revisions; four offline holding peers.

| Direction | File | Bytes | Upload sync | Receive sync |
| --- | --- | ---: | ---: | ---: |
| browser-to-go | relaybench-2026-10-12T20-15-34-392Z-e2dcce-browser-binary-8MiB-r1.bin | 8388608 | 6.011s | 5.369s |
| browser-to-go | relaybench-2026-10-12T20-15-34-392Z-e2dcce-browser-binary-32MiB-r1.bin | 33554432 | 11.475s | 11.681s |
| browser-to-go | relaybench-2026-10-12T20-15-34-392Z-e2dcce-browser-binary-64MiB-r1.bin | 67108864 | 18.094s | 22.113s |
| go-to-browser | relaybench-2026-10-12T20-15-34-392Z-e2dcce-go-binary-8MiB.bin | 8388608 | 5.489s | 6.238s |

Browser times include the original 200 ms wait, 100 ms polling, 150 ms settling, and UI automation. Go harness times include one final settings check; production_sync_ms and settings_read_ms in results.json and CSV separate these durations. HTTP error traces show rate limiting separately. Both exclude import and polling delays. This run uses a fresh synthetic account, rather than the private starting collection in the October 2026 reports.

Local request burst allowance: hosted configuration unchanged. Synthetic history and optional injected latency are recorded in results.json. Request traces contain methods, paths, statuses and elapsed times; no credentials or account bytes.

Cleanup: 6 benchmark devices removed; 3 sessions revoked. 0 cleanup errors. The server retains the synthetic account unlock record and revision identities; it has no account-delete endpoint.

Run failed: Could not complete sync. Your local changes are kept.
