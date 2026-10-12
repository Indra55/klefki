# Relay benchmark

Relay: http://127.0.0.1:44137. Mode: local. Started: 2026-10-12T19:59:08.728Z.

24/24 received files passed SHA-256 verification. 59 synthetic initial folder revisions; four offline holding peers.

| Direction | File | Bytes | Upload sync | Receive sync |
| --- | --- | ---: | ---: | ---: |
| browser-to-go | relaybench-2026-10-12T19-59-08-728Z-6d87b0-browser-tiny-r1.txt | 1024 | 0.442s | 0.006s |
| browser-to-go | relaybench-2026-10-12T19-59-08-728Z-6d87b0-browser-tiny-r2.txt | 1024 | 0.435s | 0.006s |
| browser-to-go | relaybench-2026-10-12T19-59-08-728Z-6d87b0-browser-table-r1.csv | 65536 | 0.441s | 0.006s |
| browser-to-go | relaybench-2026-10-12T19-59-08-728Z-6d87b0-browser-table-r2.csv | 65536 | 0.440s | 0.007s |
| browser-to-go | relaybench-2026-10-12T19-59-08-728Z-6d87b0-browser-text-r1.txt | 1048576 | 0.442s | 0.013s |
| browser-to-go | relaybench-2026-10-12T19-59-08-728Z-6d87b0-browser-text-r2.txt | 1048576 | 0.443s | 0.011s |
| browser-to-go | relaybench-2026-10-12T19-59-08-728Z-6d87b0-browser-document-r1.pdf | 1048576 | 0.436s | 0.010s |
| browser-to-go | relaybench-2026-10-12T19-59-08-728Z-6d87b0-browser-document-r2.pdf | 1048576 | 0.437s | 0.014s |
| browser-to-go | relaybench-2026-10-12T19-59-08-728Z-6d87b0-browser-photo-r1.png | 1045273 | 0.445s | 0.012s |
| browser-to-go | relaybench-2026-10-12T19-59-08-728Z-6d87b0-browser-photo-r2.png | 1045273 | 0.447s | 0.016s |
| browser-to-go | relaybench-2026-10-12T19-59-08-728Z-6d87b0-browser-archive-r1.zip | 1048576 | 0.455s | 0.012s |
| browser-to-go | relaybench-2026-10-12T19-59-08-728Z-6d87b0-browser-archive-r2.zip | 1048576 | 0.455s | 0.020s |
| browser-to-go | relaybench-2026-10-12T19-59-08-728Z-6d87b0-browser-binary-8MiB-r1.bin | 8388608 | 0.707s | 0.084s |
| browser-to-go | relaybench-2026-10-12T19-59-08-728Z-6d87b0-browser-binary-32MiB-r1.bin | 33554432 | 0.690s | 0.162s |
| browser-to-go | relaybench-2026-10-12T19-59-08-728Z-6d87b0-browser-binary-64MiB-r1.bin | 67108864 | 1.067s | 0.319s |
| go-to-browser | relaybench-2026-10-12T19-59-08-728Z-6d87b0-go-tiny.txt | 1024 | 0.013s | 0.478s |
| go-to-browser | relaybench-2026-10-12T19-59-08-728Z-6d87b0-go-table.csv | 65536 | 0.018s | 0.527s |
| go-to-browser | relaybench-2026-10-12T19-59-08-728Z-6d87b0-go-text.txt | 1048576 | 0.028s | 0.572s |
| go-to-browser | relaybench-2026-10-12T19-59-08-728Z-6d87b0-go-document.pdf | 1048576 | 0.031s | 0.597s |
| go-to-browser | relaybench-2026-10-12T19-59-08-728Z-6d87b0-go-photo.png | 1045273 | 0.030s | 0.586s |
| go-to-browser | relaybench-2026-10-12T19-59-08-728Z-6d87b0-go-archive.zip | 1048576 | 0.042s | 0.573s |
| go-to-browser | relaybench-2026-10-12T19-59-08-728Z-6d87b0-go-binary-8MiB.bin | 8388608 | 0.106s | 0.707s |
| go-to-browser | relaybench-2026-10-12T19-59-08-728Z-6d87b0-go-binary-32MiB.bin | 33554432 | 0.193s | 0.677s |
| browser-to-go | relaybench-2026-10-12T19-59-08-728Z-6d87b0-duplicate.zip | 1048576 | 0.480s | 0.014s |

Browser times include the original 200 ms wait, 100 ms polling, 150 ms settling, and UI automation. Go sync times include one final settings check. Both exclude import and polling delays. This run uses a fresh synthetic account, rather than the private starting collection in the October 2026 reports.

Local request burst allowance: 12000. Synthetic history and optional injected latency are recorded in results.json. Request traces contain methods, paths, statuses and elapsed times; no credentials or account bytes.

Cleanup: 6 benchmark devices removed; 3 sessions revoked. 0 cleanup errors. The server retains the synthetic account unlock record and revision identities; it has no account-delete endpoint.

No-edit sync medians: Go 0.005s; browser UI 0.462s.
