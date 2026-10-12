# Relay benchmark

Relay: http://127.0.0.1:33575. Mode: local. Started: 2026-10-12T20:02:12.525Z.

24/24 received files passed SHA-256 verification. 59 synthetic initial folder revisions; four offline holding peers.

| Direction | File | Bytes | Upload sync | Receive sync |
| --- | --- | ---: | ---: | ---: |
| browser-to-go | relaybench-2026-10-12T20-02-12-524Z-5b85de-browser-tiny-r1.txt | 1024 | 0.435s | 0.005s |
| browser-to-go | relaybench-2026-10-12T20-02-12-524Z-5b85de-browser-tiny-r2.txt | 1024 | 0.430s | 0.007s |
| browser-to-go | relaybench-2026-10-12T20-02-12-524Z-5b85de-browser-table-r1.csv | 65536 | 0.495s | 0.016s |
| browser-to-go | relaybench-2026-10-12T20-02-12-524Z-5b85de-browser-table-r2.csv | 65536 | 0.467s | 0.015s |
| browser-to-go | relaybench-2026-10-12T20-02-12-524Z-5b85de-browser-text-r1.txt | 1048576 | 0.504s | 0.022s |
| browser-to-go | relaybench-2026-10-12T20-02-12-524Z-5b85de-browser-text-r2.txt | 1048576 | 0.481s | 0.031s |
| browser-to-go | relaybench-2026-10-12T20-02-12-524Z-5b85de-browser-document-r1.pdf | 1048576 | 0.493s | 0.019s |
| browser-to-go | relaybench-2026-10-12T20-02-12-524Z-5b85de-browser-document-r2.pdf | 1048576 | 0.488s | 0.022s |
| browser-to-go | relaybench-2026-10-12T20-02-12-524Z-5b85de-browser-photo-r1.png | 1045273 | 0.504s | 0.018s |
| browser-to-go | relaybench-2026-10-12T20-02-12-524Z-5b85de-browser-photo-r2.png | 1045273 | 0.502s | 0.025s |
| browser-to-go | relaybench-2026-10-12T20-02-12-524Z-5b85de-browser-archive-r1.zip | 1048576 | 0.524s | 0.022s |
| browser-to-go | relaybench-2026-10-12T20-02-12-524Z-5b85de-browser-archive-r2.zip | 1048576 | 0.529s | 0.018s |
| browser-to-go | relaybench-2026-10-12T20-02-12-524Z-5b85de-browser-binary-8MiB-r1.bin | 8388608 | 0.676s | 0.077s |
| browser-to-go | relaybench-2026-10-12T20-02-12-524Z-5b85de-browser-binary-32MiB-r1.bin | 33554432 | 0.709s | 0.161s |
| browser-to-go | relaybench-2026-10-12T20-02-12-524Z-5b85de-browser-binary-64MiB-r1.bin | 67108864 | 1.064s | 0.344s |
| go-to-browser | relaybench-2026-10-12T20-02-12-524Z-5b85de-go-tiny.txt | 1024 | 0.009s | 0.459s |
| go-to-browser | relaybench-2026-10-12T20-02-12-524Z-5b85de-go-table.csv | 65536 | 0.012s | 0.492s |
| go-to-browser | relaybench-2026-10-12T20-02-12-524Z-5b85de-go-text.txt | 1048576 | 0.038s | 0.574s |
| go-to-browser | relaybench-2026-10-12T20-02-12-524Z-5b85de-go-document.pdf | 1048576 | 0.039s | 0.560s |
| go-to-browser | relaybench-2026-10-12T20-02-12-524Z-5b85de-go-photo.png | 1045273 | 0.033s | 0.567s |
| go-to-browser | relaybench-2026-10-12T20-02-12-524Z-5b85de-go-archive.zip | 1048576 | 0.039s | 0.570s |
| go-to-browser | relaybench-2026-10-12T20-02-12-524Z-5b85de-go-binary-8MiB.bin | 8388608 | 0.106s | 0.706s |
| go-to-browser | relaybench-2026-10-12T20-02-12-524Z-5b85de-go-binary-32MiB.bin | 33554432 | 0.336s | 0.699s |
| browser-to-go | relaybench-2026-10-12T20-02-12-524Z-5b85de-duplicate.zip | 1048576 | 0.482s | 0.013s |

Browser times include the original 200 ms wait, 100 ms polling, 150 ms settling, and UI automation. Go harness times include one final settings check; production_sync_ms and settings_read_ms in results.json and CSV separate these durations. HTTP error traces show rate limiting separately. Both exclude import and polling delays. This run uses a fresh synthetic account, rather than the private starting collection in the October 2026 reports.

Local request burst allowance: 12000. Synthetic history and optional injected latency are recorded in results.json. Request traces contain methods, paths, statuses and elapsed times; no credentials or account bytes.

Cleanup: 6 benchmark devices removed; 3 sessions revoked. 0 cleanup errors. The server retains the synthetic account unlock record and revision identities; it has no account-delete endpoint.

No-edit sync medians: Go 0.005s; browser UI 0.461s.

| Delay per request | Median Go elapsed | Production sync | Requests |
| --- | ---: | ---: | ---: |
| 0 ms | 0.004s | 0.003594s | 4 |
| 100 ms | 0.406s | 0.304871s | 4 |
| 300 ms | 1.211s | 0.909648s | 4 |
