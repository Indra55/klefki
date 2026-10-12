# Relay latency optimization

The server and clients are deployed at `https://klefki.onrender.com`. Deployment order: compatible server `1521890a38e64b6c364f3e61b4603b1664e09afa`, then Go/browser clients `a72fcbd1b998122974d4769716de118265e966be`. Render reported successful deployments for both. Updated browser asset: `/assets/index-CGrCuk_W.js`.

## Protocol and validation

An unchanged account with 84 retained revisions uses three production requests: prepare, changes, explicit durable acknowledgement. The old client used ten. Tests cover 0, 84, 256, 257 and 769 revisions; both 404 and 405 legacy fallback; unchanged legacy JSON shapes and page limits; 16 KiB batch body limits; late joining, restored snapshots, complete server loss/epoch changes, persistent restoration, forgotten devices, quota exhaustion and cleanup.

Chunks remain 4 MiB and retain their existing authentication/encryption format. Go and browser clients transfer windows of at most four chunks and publish revisions sequentially only after every chunk succeeds. Downloads authenticate each chunk before ordered staging/commit. Terminal failure cancels and joins sibling work. Workers share Retry-After cooldowns. Account locks serialize quotas, upload pins, publication and cleanup; bounded network reads and buffered response writes occur outside those locks.

`go test -race ./...`, `go vet ./...`, 43 browser crypto checks, all 12 relay/PWA/sync-status integration tests, web and desktop production builds, and `make linux-build` passed. Additional race tests passed for cleanup during a slow blob download, concurrent quota enforcement, cancelled sibling work and exactly four real Go upload/download requests with a matching received SHA-256. Native interactive UI acceptance is separate from the rebuilt executable.

## Preparation measured before concurrency

The saved batching-only source is [batching-source.patch](batching-source.patch), applied to base `bac11328dc598cf5e0134f01cd1e502057137896`; its complete relevant-file hashes are [batching-source-manifest.json](batching-source-manifest.json). It was measured before adding concurrency. Later full batching-only sweeps reused that isolated source snapshot.

Five samples per injected-delay level, unchanged 84-revision account, no chunks transferred:

| Added delay per request | Previous full harness | Batching production sync | Batching full harness |
| --- | ---: | ---: | ---: |
| 0 ms | 0.025s | 0.007s | 0.008s |
| 100 ms | 1.135s | 0.312s | 0.413s |
| 300 ms | 3.348s | 0.907s | 1.208s |

Production sync is measured directly around `Client.Sync`; the full Go harness includes one separate settings read. Thus four harness requests mean three production requests. The request-count integration test independently measured 907 ms with 300 ms injected into each of three requests. This is artificial per-request delay, not WAN emulation.

## Local transfer evidence

Two full batching-only sweeps and two full concurrency sweeps each passed 24/24 received-file SHA-256 checks. Identical fixture hashes and sizes were reused, each sweep used a fresh synthetic account, four offline holding peers, unchanged 256 MiB quota and isolated `RateBurst=12000` to avoid conflating transfer time with sleeps. The independent [production-rate-control](production-rate-control/results.json) used the unchanged production `RateBurst=120`, passed 5/5 hashes, and recorded a real 60-second Retry-After pause.

| Measurement, median of two sweeps | Batching only | Batching + concurrency |
| --- | ---: | ---: |
| 32 MiB browser upload, UI harness | 0.688s | 0.747s |
| 64 MiB browser upload, UI harness | 1.040s | 1.135s |
| 64 MiB Go receive, production sync | 0.321s | 0.339s |
| 32 MiB Go upload, production sync | 0.271s | 0.350s |
| 32 MiB browser receive, UI harness | 0.887s | 0.901s |

These loopback repetitions do not demonstrate a large-file concurrency speedup. Disk/process scheduling and UI waits dominate this local comparison; hosted bandwidth and request latency must be measured independently. Earlier local runs are not an attributable concurrency baseline because both batching and other harness/account details differ.

## Hosted measurements

The corrected [hosted-large2 sweep](hosted-large2/results.json) passed all six received-file SHA-256 checks, duplicate import checks, three zero-chunk no-edit checks, and cleanup with zero errors. A further [large-file repetition](hosted-large3/results.json), starting with zero synthetic history to reduce setup requests, passed four file hashes, including all three forward sizes, then stopped with a browser sync error during the reverse 32 MiB receive. Cleanup succeeded. The first focused run failed with local disk quota exceeded; generated fixtures and profiles were removed before repeating. Local quota exhaustion may explain the preceding browser error, but its exact cause was not captured. The harness now saves browser failure traces. The subsequent [focused 32 MiB sweep](hosted-32repeat/results.json) passed all three hashes, duplicate import, three zero-chunk no-edit checks and cleanup with zero errors.

| Hosted measurement | Earlier hosted baseline | Updated measurement |
| --- | ---: | ---: |
| No-edit Go full harness, median of 3 | 3.618s | 1.034s |
| No-edit Go production sync, median of 3 | Not separately recorded | 0.786s |
| No-edit browser UI, median of 3 | 4.529s | 1.481s |
| 1 KiB browser upload, median of 2 | 5.327s | 2.828s |
| 1 KiB Go receive, full harness, median of 2 | 4.364s | 2.017s |
| 32 MiB browser upload, median of 2 updated trials | 25.511s | 11.267s |
| 64 MiB browser upload, median of 2 updated trials | 40.585s | 18.801s |
| 64 MiB Go receive, full harness, median of 2 updated trials | 31.375s | 25.205s |
| 32 MiB Go upload, full harness, median of 2 updated trials | 21.796s | 10.304s |
| 32 MiB browser receive, UI harness, median of 2 updated trials | 22.820s | 10.828s |

Preparation and concurrency were isolated in local source snapshots. The live hosted follow-up measures the combined implementation; a separate hosted batching-only deployment was not measured. The older large-file baseline contains one trial per size. The updated forward large-file trials used 59 starting revisions and identical fixture bytes/sizes; three no-edit passes in the corrected sweep used 65 retained revisions. Request-count tests separately verify the target 84-revision account. Updated 64 MiB browser upload is about 54% faster in these session measurements; the no-edit full Go harness is about 71% faster. Hosting conditions and timing methodology differences prevent attributing every millisecond to the code alone.

The first updated full sweep passed 16 transfers through 64 MiB and then stopped at a reverse CSV completion-count race: its received SHA-256 matched, but the count was sampled before browser sync finished. The failed run is retained as [hosted-updated-incomplete](hosted-updated-incomplete/results.json). Acknowledgement-based completion checking replaces the early idle sample in the benchmark harness. No retry is silently substituted into the failed run. The corrected large sweep observed one Chromium `net::ERR_ABORTED` request event after an HTTP 200 chunk response; the resulting file passed its SHA-256 check. No Go HTTP errors or 429 responses occurred during the recorded corrected transfers.

The updated server keeps the free Render hosting configuration, production rate limits, quota, dependencies, polling intervals and crypto formats unchanged. Free-service deployment reset the server epoch; the existing recovery/epoch test suite passed. Hosted timings use synthetic benchmark accounts, not personal documents. The old-server compatibility sweep passed all five hashes with the updated Go client falling back after prepare returned 404; it used the previously deployed browser bundle. Its two cleanup revocations hit 429; a subsequent deployment changed the epoch and discarded that ephemeral account state. The benchmark now honors Retry-After during cleanup too.

Each results file records exact request traces, HTTP errors, fixture hashes, source revision, browser assets, production sync duration and separate settings-read duration where available. Full browser harness times include 200 ms after-click wait, 100 ms completion sampling and 150 ms settling, plus automation. Rate-limit pauses remain visible in traces and are never deducted without an explicit separate measure. Small files have two forward trials; the two sweep repetitions provide repeated large-file measurements. Results are session measurements, not long-term percentiles.

Evidence folders contain reports and measurement JSON/CSV only; synthetic account files, databases, browser profiles and credentials are excluded.

The third forward large-file sample used identical fixture bytes and zero initial synthetic history. Its 64 MiB upload took 18.094s and Go receipt took 22.113s; the three updated 64 MiB upload samples span 18.094–18.927s. Both history sizes fit a single preparation batch. This does not establish identical hosted conditions for every sample.

The focused final sweep started with zero history, used the same 32 MiB fixture hash, and measured 11.181s browser upload, 10.641s Go receipt, 10.653s Go upload and 11.382s browser receipt. Combined reverse medians above include the history-59 and history-0 corrected sweeps. Both use the same three-request preparation/changes/acknowledgement idle protocol and production hosting/rate configuration.
