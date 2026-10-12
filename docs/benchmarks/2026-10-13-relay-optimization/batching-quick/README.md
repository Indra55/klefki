# Relay benchmark

Relay: http://127.0.0.1:39137. Mode: local. Started: 2026-10-12T19:41:25.411Z.

5/5 received files passed SHA-256 verification. 77 synthetic initial folder revisions; four offline holding peers.

| Direction | File | Bytes | Upload sync | Receive sync |
| --- | --- | ---: | ---: | ---: |
| browser-to-go | relaybench-2026-10-12T19-41-25-410Z-2b9744-browser-tiny-r1.txt | 1024 | 0.438s | 0.008s |
| browser-to-go | relaybench-2026-10-12T19-41-25-410Z-2b9744-browser-text-r1.txt | 1048576 | 0.455s | 0.016s |
| go-to-browser | relaybench-2026-10-12T19-41-25-410Z-2b9744-go-tiny.txt | 1024 | 0.009s | 0.446s |
| go-to-browser | relaybench-2026-10-12T19-41-25-410Z-2b9744-go-text.txt | 1048576 | 0.015s | 0.472s |
| browser-to-go | relaybench-2026-10-12T19-41-25-410Z-2b9744-duplicate.zip | 1048576 | 0.463s | 0.019s |

Browser times include the original 200 ms wait, 100 ms polling, 150 ms settling, and UI automation. Go sync times include one final settings check. Both exclude import and polling delays. This run uses a fresh synthetic account, rather than the private starting collection in the October 2026 reports.

Local request burst allowance: 12000. Synthetic history and optional injected latency are recorded in results.json. Request traces contain methods, paths, statuses and elapsed times; no credentials or account bytes.

Cleanup: 6 benchmark devices removed; 3 sessions revoked. 0 cleanup errors. The server retains the synthetic account unlock record and revision identities; it has no account-delete endpoint.

No-edit sync medians: Go 0.009s; browser UI 0.495s.

| Delay per request | Median Go elapsed | Requests |
| --- | ---: | ---: |
| 0 ms | 0.008s | 4 |
| 100 ms | 0.413s | 4 |
| 300 ms | 1.208s | 4 |
