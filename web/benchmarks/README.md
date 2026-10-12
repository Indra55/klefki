# Repeatable relay benchmarks

Run from the repository root with Go 1.26+, Node 22.18+, and Chromium:

```sh
npm --prefix web ci --ignore-scripts --no-audit --no-fund
npm --prefix web exec playwright install chromium
make bench-local
make bench-hosted BENCH_SERVER=https://YOUR-RELAY
```

The local command builds the web app and Go helpers, starts a loopback relay on
an available port, runs the benchmark, and stops it. The hosted command uses the
PWA and API at the supplied origin. Both create a separate synthetic account;
no personal account file or password is needed. The hosted run sends real uploads.

Each full run covers TXT, CSV, PDF, PNG, ZIP and binary files from 1 KiB to 64 MiB,
both directions through the browser UI and production Go sync core, SHA-256
verification, an identical ZIP imported twice, and three syncs without edits.
Small forward transfers run twice; larger and reverse transfers run once. The
local command also samples 0, 100 and 300 ms added per HTTP request, five times
each. Results are observations rather than fixed performance assertions.

Outputs go in a new ignored `.benchmarks/<run>/` directory:

- `README.md`: timing summary and cleanup status.
- `transfers.csv`: transfer timings, request counts and integrity results.
- `results.json`: request traces, hashes, duplication checks and latency probes.
- Local server data, encrypted stores, browser profile and synthetic fixtures.

Quick checks and explicit output directories:

```sh
npm --prefix web run benchmark -- --local --quick --history 0 --skip-web-build
npm --prefix web run benchmark -- --server https://YOUR-RELAY --quick --history 0
npm --prefix web run benchmark:local -- --out /tmp/klefki-local-results
npm --prefix web run benchmark -- --help
```

`--quick` runs 1 KiB and 1 MiB text in both directions, the ZIP duplicate check,
and three no-edit syncs. `--history` defaults to 59 synthetic folder revisions;
four synthetic offline peers hold the queue throughout measurement. Use the
same options for local and hosted comparisons. This reproducible baseline uses
synthetic history; the original October 2026 runs used a private starting
collection. Fixtures are deterministic and shared between local and hosted runs.

The local relay defaults to `--rate-burst 12000` to avoid measuring rate-limit
backoff. `--rate-burst 120` uses the production default. Hosted limits are left
unchanged; HTTP 429 retries count toward elapsed times. A full hosted run transfers
about 160 MiB each way and may take several minutes, especially with throttling.

Browser times retain the original 200 ms wait, 100 ms completion polling and
150 ms settling, plus UI automation. Foreground request spans are recorded
separately. Go times include one final settings read. Import time and automatic
polling delay are excluded from sync timings. These runs do not measure native
desktop UI, OCR, indexing or semantic inference.

The runner removes only devices and sessions belonging to its synthetic account,
with offline peers removed first to release delivered ciphertext. Cleanup errors
are saved and cause a nonzero exit. The server retains the synthetic account
unlock record and revision identities because it has no account-delete endpoint.
Passwords and bearer tokens are never written to the report. The generated
password is discarded, so a new run creates a new account rather than resuming
an old device identity. Published reports can be copied out of `.benchmarks/`;
keep runtime databases, account files and browser profiles out of version control.

## Verification record

Verified on 13 October 2026: the full local command completed 24 transfers and
15 controlled latency samples; local and hosted quick runs completed five
transfers each. SHA-256, duplicate-import and no-edit checks passed in all three
runs. Each removed six benchmark devices, revoked three sessions and read back
a zero-length relay queue. Both Go helpers passed `go vet`; the local command
also rebuilt the production web app successfully. The hosted verification used
`--quick --history 0`, rather than a full hosted size sweep.
