# Relay benchmark

Relay: http://127.0.0.1:46367. Mode: local. Started: 2026-10-12T19:48:57.041Z.

24/24 received files passed SHA-256 verification. 59 synthetic initial folder revisions; four offline holding peers.

| Direction | File | Bytes | Upload sync | Receive sync |
| --- | --- | ---: | ---: | ---: |
| browser-to-go | relaybench-2026-10-12T19-48-57-040Z-e84176-browser-tiny-r1.txt | 1024 | 0.448s | 0.005s |
| browser-to-go | relaybench-2026-10-12T19-48-57-040Z-e84176-browser-tiny-r2.txt | 1024 | 0.416s | 0.006s |
| browser-to-go | relaybench-2026-10-12T19-48-57-040Z-e84176-browser-table-r1.csv | 65536 | 0.440s | 0.008s |
| browser-to-go | relaybench-2026-10-12T19-48-57-040Z-e84176-browser-table-r2.csv | 65536 | 0.435s | 0.008s |
| browser-to-go | relaybench-2026-10-12T19-48-57-040Z-e84176-browser-text-r1.txt | 1048576 | 0.422s | 0.013s |
| browser-to-go | relaybench-2026-10-12T19-48-57-040Z-e84176-browser-text-r2.txt | 1048576 | 0.415s | 0.012s |
| browser-to-go | relaybench-2026-10-12T19-48-57-040Z-e84176-browser-document-r1.pdf | 1048576 | 0.437s | 0.010s |
| browser-to-go | relaybench-2026-10-12T19-48-57-040Z-e84176-browser-document-r2.pdf | 1048576 | 0.444s | 0.012s |
| browser-to-go | relaybench-2026-10-12T19-48-57-040Z-e84176-browser-photo-r1.png | 1045273 | 0.436s | 0.011s |
| browser-to-go | relaybench-2026-10-12T19-48-57-040Z-e84176-browser-photo-r2.png | 1045273 | 0.436s | 0.014s |
| browser-to-go | relaybench-2026-10-12T19-48-57-040Z-e84176-browser-archive-r1.zip | 1048576 | 0.434s | 0.014s |
| browser-to-go | relaybench-2026-10-12T19-48-57-040Z-e84176-browser-archive-r2.zip | 1048576 | 0.488s | 0.020s |
| browser-to-go | relaybench-2026-10-12T19-48-57-040Z-e84176-browser-binary-8MiB-r1.bin | 8388608 | 0.677s | 0.081s |
| browser-to-go | relaybench-2026-10-12T19-48-57-040Z-e84176-browser-binary-32MiB-r1.bin | 33554432 | 0.686s | 0.152s |
| browser-to-go | relaybench-2026-10-12T19-48-57-040Z-e84176-browser-binary-64MiB-r1.bin | 67108864 | 1.012s | 0.322s |
| go-to-browser | relaybench-2026-10-12T19-48-57-040Z-e84176-go-tiny.txt | 1024 | 0.006s | 0.486s |
| go-to-browser | relaybench-2026-10-12T19-48-57-040Z-e84176-go-table.csv | 65536 | 0.015s | 0.540s |
| go-to-browser | relaybench-2026-10-12T19-48-57-040Z-e84176-go-text.txt | 1048576 | 0.033s | 0.579s |
| go-to-browser | relaybench-2026-10-12T19-48-57-040Z-e84176-go-document.pdf | 1048576 | 0.029s | 0.575s |
| go-to-browser | relaybench-2026-10-12T19-48-57-040Z-e84176-go-photo.png | 1045273 | 0.031s | 0.590s |
| go-to-browser | relaybench-2026-10-12T19-48-57-040Z-e84176-go-archive.zip | 1048576 | 0.026s | 0.597s |
| go-to-browser | relaybench-2026-10-12T19-48-57-040Z-e84176-go-binary-8MiB.bin | 8388608 | 0.102s | 0.712s |
| go-to-browser | relaybench-2026-10-12T19-48-57-040Z-e84176-go-binary-32MiB.bin | 33554432 | 0.350s | 1.097s |
| browser-to-go | relaybench-2026-10-12T19-48-57-040Z-e84176-duplicate.zip | 1048576 | 0.660s | 0.022s |

Browser times include the original 200 ms wait, 100 ms polling, 150 ms settling, and UI automation. Go sync times include one final settings check. Both exclude import and polling delays. This run uses a fresh synthetic account, rather than the private starting collection in the October 2026 reports.

Local request burst allowance: 12000. Synthetic history and optional injected latency are recorded in results.json. Request traces contain methods, paths, statuses and elapsed times; no credentials or account bytes.

Cleanup: 6 benchmark devices removed; 3 sessions revoked. 0 cleanup errors. The server retains the synthetic account unlock record and revision identities; it has no account-delete endpoint.

No-edit sync medians: Go 0.004s; browser UI 0.462s.
