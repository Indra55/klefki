# Relay benchmark

Relay: http://127.0.0.1:34153. Mode: local. Started: 2026-10-12T19:46:37.994Z.

5/5 received files passed SHA-256 verification. 59 synthetic initial folder revisions; four offline holding peers.

| Direction | File | Bytes | Upload sync | Receive sync |
| --- | --- | ---: | ---: | ---: |
| browser-to-go | relaybench-2026-10-12T19-46-37-993Z-1b0a8e-browser-tiny-r1.txt | 1024 | 0.507s | 0.121s |
| browser-to-go | relaybench-2026-10-12T19-46-37-993Z-1b0a8e-browser-text-r1.txt | 1048576 | 0.500s | 0.018s |
| go-to-browser | relaybench-2026-10-12T19-46-37-993Z-1b0a8e-go-r1-tiny.txt | 1024 | 60.014s | 0.445s |
| go-to-browser | relaybench-2026-10-12T19-46-37-993Z-1b0a8e-go-r1-text.txt | 1048576 | 0.021s | 0.468s |
| browser-to-go | relaybench-2026-10-12T19-46-37-993Z-1b0a8e-duplicate.zip | 1048576 | 0.454s | 0.017s |

Browser times include the original 200 ms wait, 100 ms polling, 150 ms settling, and UI automation. Go harness times include one final settings check; production_sync_ms and settings_read_ms in results.json and CSV separate these durations. HTTP error traces show rate limiting separately. Both exclude import and polling delays. This run uses a fresh synthetic account, rather than the private starting collection in the October 2026 reports.

Local request burst allowance: 120. Synthetic history and optional injected latency are recorded in results.json. Request traces contain methods, paths, statuses and elapsed times; no credentials or account bytes.

Cleanup: 6 benchmark devices removed; 3 sessions revoked. 0 cleanup errors. The server retains the synthetic account unlock record and revision identities; it has no account-delete endpoint.

No-edit sync medians: Go 0.005s; browser UI 0.444s.
