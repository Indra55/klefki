# Relay benchmark

Relay: https://klefki.onrender.com. Mode: hosted. Started: 2026-10-12T20:03:55.897Z.

16/17 received files passed SHA-256 verification. 59 synthetic initial folder revisions; four offline holding peers.

| Direction | File | Bytes | Upload sync | Receive sync |
| --- | --- | ---: | ---: | ---: |
| browser-to-go | relaybench-2026-10-12T20-03-55-896Z-49d16c-browser-tiny-r1.txt | 1024 | 3.004s | 2.013s |
| browser-to-go | relaybench-2026-10-12T20-03-55-896Z-49d16c-browser-tiny-r2.txt | 1024 | 2.652s | 2.021s |
| browser-to-go | relaybench-2026-10-12T20-03-55-896Z-49d16c-browser-table-r1.csv | 65536 | 2.783s | 2.091s |
| browser-to-go | relaybench-2026-10-12T20-03-55-896Z-49d16c-browser-table-r2.csv | 65536 | 2.777s | 2.560s |
| browser-to-go | relaybench-2026-10-12T20-03-55-896Z-49d16c-browser-text-r1.txt | 1048576 | 3.723s | 3.410s |
| browser-to-go | relaybench-2026-10-12T20-03-55-896Z-49d16c-browser-text-r2.txt | 1048576 | 3.225s | 2.431s |
| browser-to-go | relaybench-2026-10-12T20-03-55-896Z-49d16c-browser-document-r1.pdf | 1048576 | 3.516s | 3.519s |
| browser-to-go | relaybench-2026-10-12T20-03-55-896Z-49d16c-browser-document-r2.pdf | 1048576 | 3.093s | 3.537s |
| browser-to-go | relaybench-2026-10-12T20-03-55-896Z-49d16c-browser-photo-r1.png | 1045273 | 3.162s | 3.521s |
| browser-to-go | relaybench-2026-10-12T20-03-55-896Z-49d16c-browser-photo-r2.png | 1045273 | 4.084s | 3.507s |
| browser-to-go | relaybench-2026-10-12T20-03-55-896Z-49d16c-browser-archive-r1.zip | 1048576 | 3.203s | 3.512s |
| browser-to-go | relaybench-2026-10-12T20-03-55-896Z-49d16c-browser-archive-r2.zip | 1048576 | 3.113s | 3.491s |
| browser-to-go | relaybench-2026-10-12T20-03-55-896Z-49d16c-browser-binary-8MiB-r1.bin | 8388608 | 5.229s | 4.960s |
| browser-to-go | relaybench-2026-10-12T20-03-55-896Z-49d16c-browser-binary-32MiB-r1.bin | 33554432 | 11.596s | 11.311s |
| browser-to-go | relaybench-2026-10-12T20-03-55-896Z-49d16c-browser-binary-64MiB-r1.bin | 67108864 | 18.927s | 26.278s |
| go-to-browser | relaybench-2026-10-12T20-03-55-896Z-49d16c-go-tiny.txt | 1024 | 2.574s | 2.253s |
| go-to-browser | relaybench-2026-10-12T20-03-55-896Z-49d16c-go-table.csv | 65536 | 2.661s | 0.439s |

Browser times include the original 200 ms wait, 100 ms polling, 150 ms settling, and UI automation. Go harness times include one final settings check; production_sync_ms and settings_read_ms in results.json and CSV separate these durations. HTTP error traces show rate limiting separately. Both exclude import and polling delays. This run uses a fresh synthetic account, rather than the private starting collection in the October 2026 reports.

Local request burst allowance: hosted configuration unchanged. Synthetic history and optional injected latency are recorded in results.json. Request traces contain methods, paths, statuses and elapsed times; no credentials or account bytes.

Cleanup: 6 benchmark devices removed; 3 sessions revoked. 0 cleanup errors. The server retains the synthetic account unlock record and revision identities; it has no account-delete endpoint.

Run failed: Integrity or duplicate check failed: relaybench-2026-10-12T20-03-55-896Z-49d16c-go-table.csv
