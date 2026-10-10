# M7 — Local search and OCR

Completed 2026-10-10. Desktop document search and the CLI now search an encrypted,
rebuildable BM25 index. OCR and text extraction run on the client.

## Use

Install Tesseract with English language data and Poppler's `pdfinfo`, `pdfimages`,
`pdftotext`, and `pdftoppm` on the client PATH. These tools are already installed on this Linux
host. Desktop uses English; the CLI can select other installed languages.

In the rebuilt desktop app, upload and search normally. Each upload batch
automatically indexes the saved documents before the operation finishes. The
app shows an indexing phase and a completion toast with the measured time for
extraction, OCR, and encrypted index publication (excluding upload and queue
time). This currently rebuilds the whole index once per batch. Failed imports
do not stop successfully saved files from being indexed; indexing failure or
cancellation keeps uploaded documents and does not show a success toast.
Desktop also rebuilds a missing or stale index on the next content search after
edits, trash changes, and sync. That search waits for extraction.
Content matches are ranked; existing name/tag matches remain available.
**Rebuild search index** also allows an explicit rebuild. Explicit rebuild is
cancellable through operation controls; locking cancels work and clears results.

CLI, with flags before positional arguments:

```sh
go build -o klefki ./cli
./klefki index --klefki /private/klefki/account.v1 --language eng
./klefki search --klefki /private/klefki/account.v1 --query-file /private/query.txt
./klefki sync --klefki /private/klefki/account.v1 --server https://your-klefki-server
```

Search reads standard input if `--query-file` is omitted. Passwords use the normal
terminal prompt or `--password-file`; local recovery supports `--recovery-file`.
Close desktop before opening its SQLite store with the CLI.

## Storage and behavior

- Index payload version 1 contains current document object/revision IDs, token
  frequencies, document lengths, and the extraction language. Search uses BM25
  with k1=1.2, b=0.75, positive smoothed IDF, and deterministic tie ordering.
  Unicode letters/numbers are lowercased; query terms use OR matching. There is
  no stemming, phrase matching, semantic search, or snippet extraction.
- The index is an ordinary encrypted file named `search-index.v1.json`, tagged
  `klefki:system:search-index` inside encrypted metadata. Existing master-key
  wrapping, file-key encryption, chunks, and sync are reused. Crypto formats,
  key hierarchy, server APIs, and dependencies were not changed.
- Current nontrashed file heads are indexed, including conflict copies. Index
  files and trash descendants are excluded. Revision IDs bind the index to its
  source documents; the core rejects stale indexes and desktop rebuilds them
  before retrying search. Rebuild failures retain the
  prior index and original documents. Index revisions use normal conflict/history
  preservation, so old encrypted indexes may remain in history.
- PNG/JPEG images use local Tesseract. Images are decoded and re-encoded before
  OCR to prevent filename-list inputs. UTF-8 TXT/MD/CSV/JSON/LOG files use direct
  text extraction. PDFs extract all embedded text in one pass. On mixed pages,
  individual images receive OCR, cached per image object for this document only.
  Repeated artwork reuses OCR results. Scanned pages without embedded words and
  pages with image masks retain whole-page rendering to preserve displayed
  rotation/masking. OCR adds words absent from embedded text, avoiding doubled
  frequencies for words already in a text layer. Unsupported formats retain
  name/tag search and are counted
  explicitly as metadata-only by rebuild reports.
- Limits: 64 MiB source documents, 16 million image pixels, 100 PDF pages, 2 MiB
  extracted text per document, 32 MiB index payload, 4096-byte queries, and up to
  64 distinct query terms. PDF image extraction additionally limits each page to
  128 image entries and 32 million aggregate pixels, with a 16 million pixel limit
  per image. Extraction has a three-minute deadline per document.
- PDF extraction uses a private temporary directory, removed on handled exit.
  Abrupt termination can leave plaintext staging files; use a private RAM-backed
  temporary location when required. Go strings, decoded images, process memory,
  swap, and external tool memory cannot be comprehensively zeroed. Existing
  endpoint/temporary-file threat-model exclusions apply.
- Synced clients can search without OCR tools. M6 backups include the index.
  Desktop and web libraries hide internal index files. Web currently keeps its
  name/tag search; browser OCR/content-search UI is not included.

## Evidence

- Notification layout correction: successful indexed uploads dismiss their
  transfer cards and leave one timed success toast. Active transfers and toasts
  share a responsive stack; failed transfers remain dismissible. Removed the
  desktop minimum page width and made narrow layouts wrap. Eighteen frontend
  tests, the production build, and five Chromium fixture checks passed, including
  no notification overlap or horizontal overflow at 320, 390, 600, and 800 pixels.
  Linux and Windows binaries in `desktop/build/bin` were rebuilt. Browser checks
  use the explicit fixture bridge; no new native runtime check is claimed.
- Automatic upload indexing passed `KLEFKI_REQUIRE_OCR=1 go test -race
  ./core/desktop ./core/search` and focused `go vet`. Tests query the saved index
  directly before any desktop search, verify one rebuild for a partially
  successful batch, and retain uploads after extraction failure or cancellation.
  Seventeen frontend tests and the production frontend build passed, including
  measured-duration toasts, dismissal, failure feedback, and stale events after
  lock. Linux and Windows binaries in `desktop/build/bin` were rebuilt; this
  update has not been manually exercised in a native window.
- Required real OCR acceptance passed on a synthetic PNG scan and a two-page
  image-only PDF. Search found words on both PDF pages.
- TLS integration synced the ordinary encrypted index to a second Go client,
  which searched with an empty PATH. A restored SQLite snapshot also searched.
- Scans of server database/WAL/blob storage and local stores found none of the
  known synthetic index words or private metadata in plaintext. PDF staging was
  removed after extraction.
- `KLEFKI_REQUIRE_OCR=1 go test -race ./...` passed. After final image-input
  validation, required real OCR tests passed again. `go vet ./...` passed.
- Fourteen desktop frontend interaction tests passed, including content matches,
  rebuild action, stale-index feedback, and results arriving after lock.
- Desktop and web production frontend builds passed.
- Chromium fixture acceptance passed for content search, layout, themes, and lock.
  On this host's Node 26, run from `desktop/frontend` with
  `PW_DISABLE_TS_ESM=1 node node_modules/@playwright/test/cli.js test`.
- Linux native desktop compilation and Windows desktop cross-compilation passed,
  producing `/tmp/klefki-desktop-m7` and `/tmp/klefki-desktop-m7.exe`. These are build
  checks; no new native manual walkthrough or Windows OCR runtime test is claimed.
- No commits were created during implementation; repository contents were already
  untracked. The work is now committed as part of reconstructed history.

### Build and upload-search correction

The initial M7 builds were placed only in `/tmp`; the normal `desktop/build/bin`
artifacts still contained the older app. The corrected binaries are built in
`desktop/build/bin/klefki-desktop` and `desktop/build/bin/klefki-desktop.exe`.
Desktop now automatically rebuilds on search when the index is missing/stale.
Focused race regression tests passed for importing the synthetic PNG scan and
finding an OCR word without clicking rebuild, and for searching after an edit.
The production frontend build passed. Quit the previously running app before
launching the replacement binary; the app uses a single-instance lock.

Reproduce core acceptance with
`KLEFKI_REQUIRE_OCR=1 go test ./core/search -count=1`. Normal tests skip real OCR
acceptance when tools are absent; the environment variable makes it mandatory.
Fixtures are synthetic scans generated with Pillow and the local Nimbus Sans font.

Algorithm/tool references: [BM25](https://nlp.stanford.edu/IR-book/html/htmledition/okapi-bm25-a-non-binary-model-1.html)
and [Tesseract CLI](https://tesseract-ocr.github.io/tessdoc/Command-Line-Usage.html).

### Mixed PDF extraction correction

Pages containing raster images receive OCR even if they also have selectable
text. The regression fixture has a selectable heading and an embedded scan with
different words; both are found by search. Real OCR and encrypted sync acceptance
passed with this fixture, and focused race checks passed for search, desktop, and
CLI. Older indexes lack the new encrypted extraction marker and are treated as
stale, so desktop automatically rebuilds them on the next content search. The
index payload and crypto formats are unchanged. Existing v1 rollback/deletion
limits remain. OCR quality depends on source images and installed language data.

### PDF indexing speed improvement

The subsequent optimization extracts embedded text once, OCRs individual images
on mixed pages, and reuses results for repeated PDF image objects. Pure scans and
masked composites still use rendered-page OCR. The new encrypted extraction tag
invalidates older indexes; desktop rebuilds automatically on the next search.
No new dependency or crypto/index payload format was added. The cache exists only
for one document extraction and does not add files to the klefki. Temporary images
are removed on handled exit, and cache buffers are wiped when extraction returns.

A user-selected 55-page PDF (1,039,554 bytes, 17,390 embedded words, five distinct
image objects) indexed in 2.195 seconds in the final run, versus 170.048 seconds
with the former whole-page approach. Timing covers encrypted-store reads,
extraction/OCR, BM25 term generation, and encrypted index publication, excluding
initial import and app unlock. Another development run took 1.859 seconds; these
are individual measurements on this host, not a generalized performance guarantee.
The final saved index was 41,691 bytes with 3,406 distinct terms. Verification
confirmed that every embedded term and its frequency survived in the saved index.
No source document or extracted plaintext was copied into the repository.

Required real OCR race tests passed for search, desktop, and CLI. The repeated
mixed-page fixture proves OCR executes once for a shared image while both pages
retain its recognized terms. Scanned/mixed-page search and encrypted sync tests
still pass. Image bounds, merging, and old-index upgrades are covered; static
checks passed. Linux and Windows desktop binaries were rebuilt in the normal
`desktop/build/bin` folder. Windows compilation remains distinct from runtime proof.
