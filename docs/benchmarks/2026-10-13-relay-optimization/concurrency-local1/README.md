# Relay benchmark

Relay: http://127.0.0.1:40633. Mode: local. Started: 2026-10-12T19:46:35.482Z.

24/24 received files passed SHA-256 verification. 59 synthetic initial folder revisions; four offline holding peers.

| Direction | File | Bytes | Upload sync | Receive sync |
| --- | --- | ---: | ---: | ---: |
| browser-to-go | relaybench-2026-10-12T19-46-35-481Z-6644c4-browser-tiny-r1.txt | 1024 | 0.458s | 0.009s |
| browser-to-go | relaybench-2026-10-12T19-46-35-481Z-6644c4-browser-tiny-r2.txt | 1024 | 0.476s | 0.011s |
| browser-to-go | relaybench-2026-10-12T19-46-35-481Z-6644c4-browser-table-r1.csv | 65536 | 0.473s | 0.012s |
| browser-to-go | relaybench-2026-10-12T19-46-35-481Z-6644c4-browser-table-r2.csv | 65536 | 0.651s | 0.054s |
| browser-to-go | relaybench-2026-10-12T19-46-35-481Z-6644c4-browser-text-r1.txt | 1048576 | 0.629s | 0.030s |
| browser-to-go | relaybench-2026-10-12T19-46-35-481Z-6644c4-browser-text-r2.txt | 1048576 | 0.501s | 0.024s |
| browser-to-go | relaybench-2026-10-12T19-46-35-481Z-6644c4-browser-document-r1.pdf | 1048576 | 0.470s | 0.020s |
| browser-to-go | relaybench-2026-10-12T19-46-35-481Z-6644c4-browser-document-r2.pdf | 1048576 | 0.460s | 0.020s |
| browser-to-go | relaybench-2026-10-12T19-46-35-481Z-6644c4-browser-photo-r1.png | 1045273 | 0.465s | 0.019s |
| browser-to-go | relaybench-2026-10-12T19-46-35-481Z-6644c4-browser-photo-r2.png | 1045273 | 0.440s | 0.018s |
| browser-to-go | relaybench-2026-10-12T19-46-35-481Z-6644c4-browser-archive-r1.zip | 1048576 | 0.474s | 0.017s |
| browser-to-go | relaybench-2026-10-12T19-46-35-481Z-6644c4-browser-archive-r2.zip | 1048576 | 0.478s | 0.019s |
| browser-to-go | relaybench-2026-10-12T19-46-35-481Z-6644c4-browser-binary-8MiB-r1.bin | 8388608 | 0.580s | 0.071s |
| browser-to-go | relaybench-2026-10-12T19-46-35-481Z-6644c4-browser-binary-32MiB-r1.bin | 33554432 | 0.784s | 0.182s |
| browser-to-go | relaybench-2026-10-12T19-46-35-481Z-6644c4-browser-binary-64MiB-r1.bin | 67108864 | 1.205s | 0.336s |
| go-to-browser | relaybench-2026-10-12T19-46-35-481Z-6644c4-go-r1-tiny.txt | 1024 | 0.011s | 0.458s |
| go-to-browser | relaybench-2026-10-12T19-46-35-481Z-6644c4-go-r1-table.csv | 65536 | 0.012s | 0.500s |
| go-to-browser | relaybench-2026-10-12T19-46-35-481Z-6644c4-go-r1-text.txt | 1048576 | 0.019s | 0.471s |
| go-to-browser | relaybench-2026-10-12T19-46-35-481Z-6644c4-go-r1-document.pdf | 1048576 | 0.022s | 0.476s |
| go-to-browser | relaybench-2026-10-12T19-46-35-481Z-6644c4-go-r1-photo.png | 1045273 | 0.021s | 0.477s |
| go-to-browser | relaybench-2026-10-12T19-46-35-481Z-6644c4-go-r1-archive.zip | 1048576 | 0.019s | 0.502s |
| go-to-browser | relaybench-2026-10-12T19-46-35-481Z-6644c4-go-r1-binary-8MiB.bin | 8388608 | 0.120s | 0.716s |
| go-to-browser | relaybench-2026-10-12T19-46-35-481Z-6644c4-go-r1-binary-32MiB.bin | 33554432 | 0.364s | 1.103s |
| browser-to-go | relaybench-2026-10-12T19-46-35-481Z-6644c4-duplicate.zip | 1048576 | 0.706s | 0.016s |

Browser times include the original 200 ms wait, 100 ms polling, 150 ms settling, and UI automation. Go harness times include one final settings check; production_sync_ms and settings_read_ms in results.json and CSV separate these durations. HTTP error traces show rate limiting separately. Both exclude import and polling delays. This run uses a fresh synthetic account, rather than the private starting collection in the October 2026 reports.

Local request burst allowance: 12000. Synthetic history and optional injected latency are recorded in results.json. Request traces contain methods, paths, statuses and elapsed times; no credentials or account bytes.

Cleanup: 6 benchmark devices removed; 3 sessions revoked. 0 cleanup errors. The server retains the synthetic account unlock record and revision identities; it has no account-delete endpoint.

No-edit sync medians: Go 0.006s; browser UI 0.466s.

| Delay per request | Median Go elapsed | Production sync | Requests |
| --- | ---: | ---: | ---: |
| 0 ms | 0.005s | 0.004223s | 4 |
| 100 ms | 0.408s | 0.307081s | 4 |
| 300 ms | 1.211s | 0.9094030000000001s | 4 |
