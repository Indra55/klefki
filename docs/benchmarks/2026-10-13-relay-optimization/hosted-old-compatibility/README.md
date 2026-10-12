# Relay benchmark

Relay: https://klefki.onrender.com. Mode: hosted. Started: 2026-10-12T19:50:36.906Z.

5/5 received files passed SHA-256 verification. 59 synthetic initial folder revisions; four offline holding peers.

| Direction | File | Bytes | Upload sync | Receive sync |
| --- | --- | ---: | ---: | ---: |
| browser-to-go | relaybench-2026-10-12T19-50-36-906Z-45e2d2-browser-tiny-r1.txt | 1024 | 3.984s | 3.570s |
| browser-to-go | relaybench-2026-10-12T19-50-36-906Z-45e2d2-browser-text-r1.txt | 1048576 | 5.150s | 4.995s |
| go-to-browser | relaybench-2026-10-12T19-50-36-906Z-45e2d2-go-r1-tiny.txt | 1024 | 4.183s | 3.528s |
| go-to-browser | relaybench-2026-10-12T19-50-36-906Z-45e2d2-go-r1-text.txt | 1048576 | 4.717s | 3.799s |
| browser-to-go | relaybench-2026-10-12T19-50-36-906Z-45e2d2-duplicate.zip | 1048576 | 5.170s | 4.085s |

Browser times include the original 200 ms wait, 100 ms polling, 150 ms settling, and UI automation. Go harness times include one final settings check; production_sync_ms and settings_read_ms in results.json and CSV separate these durations. HTTP error traces show rate limiting separately. Both exclude import and polling delays. This run uses a fresh synthetic account, rather than the private starting collection in the October 2026 reports.

Local request burst allowance: hosted configuration unchanged. Synthetic history and optional injected latency are recorded in results.json. Request traces contain methods, paths, statuses and elapsed times; no credentials or account bytes.

Cleanup: 6 benchmark devices removed; 1 sessions revoked. 2 cleanup errors. The server retains the synthetic account unlock record and revision identities; it has no account-delete endpoint.

No-edit sync medians: Go 2.836s; browser UI 2.894s.
