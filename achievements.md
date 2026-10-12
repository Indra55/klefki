# Klefki achievements

Implemented and evidenced as of 2026-10-13. These are project achievements,
not claims that the techniques are new inventions.

## 1. PDF indexing: 170 seconds → 2.2 seconds

A 55-page PDF indexed in **2.195 seconds**, down from **170.048 seconds**,
about **77× faster**. Extracting embedded text once and reusing OCR for repeated
image objects removed unnecessary whole-page OCR. Every embedded word and its
frequency was retained; the saved index occupied **41,691 bytes**.

Measured on this host for one PDF with selectable text and repeated images;
timing includes extraction, term generation, and encrypted index storage.
Fully scanned PDFs still require more OCR. [Evidence](docs/m7-status.md).

## 2. Search that stays encrypted across devices

Local Tesseract extraction feeds a BM25 index stored as an ordinary encrypted
klefki file. A second client searched the synced index with OCR tools unavailable.
Mixed-page tests found both selectable text and words inside scans; storage checks
found no known indexed words in plaintext. [Evidence](docs/m7-status.md).

## 3. Compatible encryption in Go and TypeScript

Independent crypto implementations share the same versioned records and test
vectors. Chromium acceptance transferred a multichunk document from the desktop
service to web byte-for-byte, and a web upload opened through the desktop service.
[Evidence](docs/m5-status.md).

## 4. Offline conflicts preserve both versions

Two devices editing the same document offline retain both contents as conflict
copies. Interrupted multichunk uploads survive client/server restarts and resume
without resending already accepted chunks. Corrupt downloads fail authentication
without publishing a partial revision or advancing the local cursor.
[Evidence](docs/m3-status.md).

## 5. Exact restore without the original installation

A fresh, network-disabled Linux container restored encrypted backups with either
the password or recovery code. Every account byte, database row, exported
document, and manifest matched, including retained revision and pending-sync
state. Integrity checks cover historical chunks as well as current documents.
[Evidence](docs/m6-status.md).

## 6. Chunk integrity binds content to its place

Authenticated chunks bind account, file, index, and final-chunk status. Tests
reject swapped, missing, truncated, appended, and mixed-revision chunks. Seven
parser fuzz targets completed approximately **1.51 million executions** during
short smoke runs. This is validation evidence, not a security audit or rollback
protection. [Evidence](docs/m1-status.md).

## 7. Native Linux fingerprint unlock

The desktop app integrates Linux fingerprint verification with local keyring
storage and reader-contact feedback. The user verified physical unlock and
restart behavior; native desktop acceptance was also confirmed on Linux and
Windows. Fingerprint support is Linux-specific.
[Evidence](docs/m4-status.md).

## 8. Faster sync on the same free hosted relay

Hosted no-edit Go sync fell from **3.618 seconds to 1.034 seconds**, about
**71% less time**. A 64 MiB browser upload fell from **40.585 seconds to
18.801 seconds**, about **54% less time**; 32 MiB browser receipt fell from
**22.820 seconds to 10.828 seconds**.

Combined preparation and 256-ID history batches reduced an unchanged
84-revision sync from **10 production requests to 3**. Four authenticated
chunk transfers run concurrently, with sequential revision publication and
explicit acknowledgement after durable local receipt. The compatible server
was deployed before the updated clients.

All nine received-file SHA-256 checks passed across two corrected hosted
sweeps, alongside Go race tests, vet, 43 browser crypto checks, and 12 browser
integration tests. Hosting, production rate limits, and encryption formats
were unchanged. These are session measurements; hosted timings cover the
combined update, and interrupted runs and measurement limits are retained.
[Evidence](docs/benchmarks/2026-10-13-relay-optimization/README.md).
