# M8 — Optional local semantic search and classification

Implemented 2026-10-10. **Private-document acceptance is pending; M8 remains
unchecked.** The public synthetic evaluation is not evidence of performance on
personal documents or a security audit.

## Use

M8 is off by default on each vault/device. Desktop Settings offers **Semantic
search and suggestions**, a cancellable model download, and selection of a
verified local bundle. Enabling builds the additional encrypted index. Documents
continue to be saved if indexing fails. Disable to stop semantic processing;
existing keyword search, documents, and saved corrections remain available.
Confirmed categories supplement category filters while the feature is enabled.
Document details show reviewable categories, generic dates and amounts with
source evidence. Nothing automatically changes existing tags. No expiry-date
extraction or reminders are included.

The normal Linux and Windows desktop binaries in `desktop/build/bin` include
native ONNX support. Quit the old app before opening the replacement binary.
Inference is CPU-only. Model assets live under the app's settings directory,
`models/<fingerprint>`, separately from encrypted vault data.

BGE-small replaces MiniLM. Existing enabled devices retain their opt-in, and
classification thresholds reset to the BGE defaults when the model fingerprint
changes. Restart the rebuilt desktop app. If its BGE bundle is not installed,
Settings offers **Install BGE-small model**; setup rebuilds the encrypted indexes.
With the bundle already installed, the first search rebuilds a stale index.
Saved corrections remain applicable to unchanged document content. Browser
devices install the new model separately and require a newly synced desktop/CLI
index. Old model assets are not treated as the current model.

CLI examples (flags precede positional arguments):

```sh
go build -tags onnx -o vault ./cli
./vault model status
./vault model setup                          # download verified public assets
./vault model setup --bundle /private/model  # or an existing verified directory
./vault index --vault /private/account.v1 --semantic
./vault search --vault /private/account.v1 --mode hybrid --query-file /private/query.txt
./vault analyze --vault /private/account.v1 --revision REVISION_ID
./vault correct --vault /private/account.v1 --revision REVISION_ID --correction-file /private/correction.json
```

`--model-dir DIR` overrides the CLI model directory. `index` and `search` without
M8 flags retain keyword-only behavior. Queries read stdin when `--query-file` is
absent. Normal password/recovery-file conventions remain; close desktop before
opening its store with CLI. Explicit hybrid commands report an error when the
model/index is unavailable rather than claiming keyword output is hybrid.

Corrections use `{ "category": "Money", "fields": [], "resolve": [] }`.
Copy the evidence-bearing `fields` from analysis and edit their `value` to keep
source evidence. For competing corrections, include every active conflicting
correction revision in `resolve` after reviewing the selected values.

## Browser deployment and use

Prepare model assets on the server's public web build before exposing the option:

```sh
VAULT_MODEL_DIR=/path/to/verified/model/bundle npm --prefix web run build
```

The build verifies model assets, copies them under `models/bge-small`, and packages
pinned browser WASM/runtime assets. Serve `web/dist` through the existing Vault
server. A build without a supplied model bundle remains usable for ordinary web
features, but activation reports unavailable assets until they are provisioned.
Model preparation is a deployment step; clients download assets only on opt-in.

Browser users enable M8 independently in Vault settings. Setup downloads about
139 MiB of public model/tokenizer/WASM assets from the same origin, validates each
checksum, and stores them in a separate model cache. A local bundle can supply
model/tokenizer assets; browser runtime assets still come from the app origin.
No model downloads occur by default. The service worker serves cached model
runtime assets offline without precaching models into the app shell.

Desktop/CLI must extract and sync the document indexes. Web performs query
embeddings, ranking, analysis display and corrections locally; it does not add
browser OCR or independent document indexing. Web-only uploads keep name/tag
search until a desktop/CLI processes them. Missing/stale model or index state has
visible status. Lock/disable terminates the worker and clears private UI state.

## Storage and behavior

- Model: English `BAAI/bge-small-en-v1.5`, immutable revision
  `5c38ec7c405ec4b44b94cc5a9bb96e735b38267a`; FP32 graph, 384 dimensions,
  first-token (CLS) pooling and L2 normalization. Native ONNX Runtime 1.23.2;
  Go wrapper 1.25.0 and pure-Go tokenizer 0.3.0. Browser ONNX Runtime 1.23.2
  and Transformers.js 3.8.1. SHA-256 and byte sizes are pinned in manifests.
  Retrieval uses unprefixed text: both the recommended query instruction and
  unprefixed input were evaluated, and unprefixed input was selected for this
  app's full-sentence queries. BGE v1.5 supports either setting. See the
  [publisher's pooling and retrieval guidance](https://huggingface.co/BAAI/bge-small-en-v1.5).
- Semantic payload v1 binds object, revision and content file ID to name vectors,
  480-token content windows with 64-token overlap, averaged content vectors,
  six description-based label vectors, and evidence-bearing fields. Model,
  tokenizer, chunking/pooling and extraction identities detect stale data.
- `semantic-index.v1.json`, tagged `vault:system:semantic-index`, is rebuildable.
  `analysis-correction.v1.json`, tagged `vault:system:analysis-correction`, is
  durable and binds corrections to object/content file IDs. These are ordinary
  encrypted vault files; existing crypto containers, key hierarchy, BM25 payload
  and sync APIs are unchanged. Internal files and trash descendants are excluded.
- Metadata-only edits preserve corrections; content replacement invalidates
  their applicability. Rebuilds preserve correction records. Concurrent
  corrections have no automatic winner; explicit resolution supersedes reviewed
  records. Old encrypted records remain present and are included in backups.
- Keyword queries omit common English function words without changing stored
  document terms. Questions with at least four remaining terms require one third
  of those terms, rounded up and capped at three, to match; an exact number or
  identifier containing digits also qualifies. Short keyword searches retain
  any meaningful-word matching.
- Semantic search uses best-chunk cosine similarity with candidate cutoff
  max(0.52, 90% of the strongest candidate). Questions with more than three
  meaningful terms use content vectors; short searches also use filename vectors.
  This rejects substantially weaker
  semantic matches before fusion discards the original cosine scores.
  Hybrid combines the top 50 keyword/vector candidates with equal-weight RRF,
  k=60, deterministic revision-ID ties, and at most 20 results. No vector database
  or ANN dependency is added. Empty queries do not start inference.
- Category descriptions include example document types. Configurable defaults
  selected on the synthetic evaluation are **cutoff 0.50, margin 0.01**. These
  are ranking thresholds, not calibrated probabilities. Threshold tuning maximizes
  micro F1, counting abstentions as misses, over 105 candidate pairs; ties prefer
  fewer mistakes, then a higher cutoff. At least
  three of up to five confirmed other-document neighbors with cosine >=0.50 and
  a weighted category majority >=0.60 activate kNN. Predicted categories never
  become training examples automatically.
- Generic date normalization supports valid ISO and English month-name dates.
  Slash dates remain raw because their ordering is ambiguous. Amounts normalize
  valid international/Indian grouping with INR/USD/EUR/GBP or unambiguous currency
  symbols. An unqualified dollar symbol retains raw text without inferred currency.
- M7 extraction, staging and document limits remain; semantic JSON is bounded to
  32 MiB. Failed/cancelled builds preserve documents and prior semantic data.
  Model assets are staged and verified before installation; model failures produce
  keyword status in desktop/web. Native runs support cancellation.

## Evaluation and evidence

[Recorded public result](m8-evaluation.json): 60 generated documents, 120 authored
labelled queries, including January/February near-duplicates, paraphrases, and
exact numeric identifiers. All modes use the same corpus and maximum ranking
depth 20; missing expected documents score zero. This is MRR over returned hits,
not unrestricted full-corpus MRR. Revision-ID tie ordering can change MRR slightly
when recreating an encrypted fixture store.

| Mode | Top-3 success | MRR at depth 20 |
|---|---:|---:|
| Keyword-only | 88.33% | 0.8505 |
| Semantic-only | 99.17% | 0.9368 |
| Hybrid | **95.00%** | **0.9275** |

Hybrid exceeds the 80% target. Classification at the selected thresholds:
57 correct, 0 incorrect, 3 abstained; 95% coverage and 100% precision on
suggested categories. Threshold selection and these classification measurements
use the same synthetic set; they are tuning results, not held-out generalization
claims. The fixture is authored from 30 document types with two reporting months.
Results above were rerun with BGE-small, longer content windows and calibrated
relevance. The preceding MiniLM result after the keyword fix was 92.50% hybrid
top-3 with MRR 0.8451. Separate regression cases verify that a long
consumption-history/payment question excludes a resume, football text and an
unrelated chart, while retaining exact identifier matches and no-match results.
An isolated local check using the user-supplied bill and two synthetic distractors
retrieved only the bill for 10 of 12 supplied questions, versus six with MiniLM
and a neutral filename. The visual-layout and arithmetic questions returned no
matches. This is a diagnostic, not full-vault acceptance or proof of chart/table
reasoning; the image and extracted private text are not stored in this repository.

Reproduce with:

```sh
go run -tags onnx ./cmd/vault-m8-eval --model-dir /path/to/model --output /tmp/m8-result.json
VAULT_MODEL_DIR=/path/to/model VAULT_REQUIRE_ONNX=1 go test -race -tags onnx ./core/search ./core/semantic ./core/desktop ./cli ./server
```

Verified:

- Real native inference; chunk/index validation; stale model/source rejection;
  cancellation and model failure; metadata-only correction preservation; content
  replacement invalidation; explicit competing-correction resolution and kNN
  excluding the document itself.
- TLS encrypted index/correction sync into a second Go client, encrypted SQLite
  snapshot restore, and absence of known synthetic document/index/field plaintext
  in local/server database and blob files.
- Chromium native/browser token IDs, fingerprint, and embeddings agree (maximum
  component difference <0.0001). Real model search works offline after reload;
  web corrections sync back to Go; internal files stay hidden; M8 is initially off.
- All three M8 browser cases passed with BGE across two fresh-server runs,
  including native/browser parity, offline search, enabled-device preference
  migration, correction sync and long-query relevance. Native preference
  migration and rejection of MiniLM analysis indexes also passed.
- Existing six web acceptance cases and the original two M8 browser cases passed across
  separate runs. All 21 desktop frontend tests and 43 web crypto checks passed;
  frontend production builds passed. Go default-build regressions, native race
  tests, and default/native static checks passed. Native model download and local
  bundle installation both completed with checksum verification.
- The new browser relevance regression passed against a fresh server: the long
  payment/consumption question returns only the utility document, exact-number
  lookup remains available, and an unrelated lunar-spacecraft question returns
  no matches. A combined nine-case run passed seven cases and hit sync setup
  timeouts in two cases before search; the server shares a 120-request/minute
  data budget per IP. Both affected cases passed when rerun against fresh
  servers, giving nine passing cases across runs.
- Linux native and Windows desktop cross-compilation passed with ONNX enabled.
  These are build checks; no new native-window or Windows inference walkthrough
  is claimed. No commits were created during implementation; repository contents were already
  untracked. The work is now committed as part of reconstructed history.

## Private acceptance still required

Use at least 50 current documents and 100 labelled queries in your own local
vault. Include near-duplicates, paraphrases and exact identifiers/numbers. Create
an external private JSON file:

```json
{
  "queries": [{"query": "your private question", "expected": ["document revision ID"]}],
  "categories": [{"revision": "document revision ID", "category": "Money"}]
}
```

The example shows the shape only; the real file must meet the sample counts.
Get current document revision IDs from `vault list`; exclude internal sidecars.
Run `vault evaluate --vault /private/account.v1 --model-dir /path/to/model
--labels-file /private/labels.json`. It reports aggregate keyword/semantic/hybrid
metrics, classification coverage/precision and tuned thresholds without emitting
queries, document names or labels. Keep inputs and reports private; record only
aggregate acceptance evidence. Require hybrid top-3 >=80%, then confirm sensible
results and category/field review in your desktop app before checking M8 complete.

## Limits

English-first; no fine-tuning, LLM, expiry interpretation, reminders or browser
OCR. Native model builds currently target Linux/Windows amd64. Model assets are
public and separately reinstallable, not vault backup contents. Browser caches
can be evicted. Older client versions do not understand M8 sidecars; upgrade
participating clients together. Documents, text, vectors, fields and corrections
are plaintext only while processing on an unlocked client. Go/JS strings, runtime
memory, external OCR staging and swap cannot be comprehensively wiped; existing
endpoint, web-delivery and v1 rollback/deletion limitations remain.
