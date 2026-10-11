# Klefki: Build Plan (for the coding agent)

Project name: `Klefki`. Personal project, single user first. This file is the source of truth for scope, architecture, and order of work. Read it fully before writing code.

## 1. What we are building

A zero-knowledge, end-to-end encrypted storage for personal documents (IDs, bills, certificates, scans).

- The server stores only ciphertext. It never sees plaintext files, filenames, folder names, or keys.
- Works on desktop (Windows + Linux), web, and later Android.
- Offline-first: every client keeps a local encrypted store, queues changes while offline, and syncs later.
- Later: on-device OCR, classification, and hybrid (keyword + semantic) search. No documents or text ever leave the device for ML.

Non-goals for now: multi-user sharing, collaboration, iOS app, server-side ML, hosted SaaS.

## 2. Stack (assumed; ask before changing)

| Part | Choice |
|---|---|
| Language | Go (server, shared core, desktop backend) |
| Server | `net/http` + `chi`, SQLite first (Postgres later if needed) |
| Blob storage | local disk behind an interface; S3-compatible (MinIO) implementation later |
| Desktop | Wails (Go backend + web frontend), targets Windows + Linux |
| Web client | TypeScript + libsodium-wrappers, installable PWA |
| Local store | SQLite on desktop and Android, IndexedDB on web |
| Android | PWA first; gomobile or native later |

Crypto libraries: Go standard library, `golang.org/x/crypto` (argon2, chacha20poly1305, curve25519). TypeScript side uses libsodium-wrappers. **Never implement crypto primitives yourself.**

Because the web client cannot reuse the Go core cheaply, crypto is implemented twice (Go, TypeScript). The two must stay byte-compatible, enforced by shared test vectors (see M0).

## 3. Repo layout

```
klefki/
  PLAN.md
  docs/
    threat-model.md
    crypto-spec.md        # exact formats, source of truth
    sync-protocol.md
  testvectors/            # JSON vectors shared by Go and TS tests
  core/                   # Go: crypto, chunking, local store, sync client
  server/                 # Go: API, auth, blob store, sync log
  cli/                    # Go: command-line client built on core
  desktop/                # Wails app
  web/                    # TypeScript PWA
```

## 4. Crypto design (summary; full spec goes in docs/crypto-spec.md)

Key hierarchy:
1. User password -> Argon2id (random per-user salt, tuned parameters stored with the account) -> 64 bytes, split into a **key-encryption key (KEK)** and a separate **auth key**. Only a derivative of the auth key is sent to the server. The password and KEK never leave the client.
2. A random 32-byte **master key** is generated at signup and stored wrapped by the KEK.
3. A **recovery key** (random 32 bytes, shown once as a printable code) also wraps the master key. Losing both password and recovery key means the data is unrecoverable by design.
4. Each file gets a random 32-byte **file key**, stored wrapped by the master key.

File encryption:
- Split into fixed-size chunks (start with 4 MiB).
- XChaCha20-Poly1305 per chunk with a random 24-byte nonce.
- Authenticated data (AAD) must bind: file id, chunk index, format version, and a final-chunk flag, so chunks cannot be reordered, dropped, truncated, or swapped between files.
- Filenames, folder names, tags, and file metadata are encrypted as separate blobs under the master key (or a derived metadata key). The server sees only opaque IDs, sizes, and timestamps.

Rules:
- Every format carries a version byte so it can be migrated.
- Use `crypto/rand` for all randomness. Zero key material when practical (best effort in Go).
- Constant-time comparisons for anything secret.
- No plaintext, keys, or passwords in logs, error messages, crash reports, or test output.

## 5. Milestones

Do them in order. Each milestone ends with passing tests and a short note in `docs/` or the README. Do not start the next one until the current one's acceptance criteria are met.

### M0: Spec and test vectors
- Write `docs/threat-model.md`: what we defend against (server breach, network attacker, stolen server disk) and what we do not (compromised client device, keylogger).
- Write `docs/crypto-spec.md` with exact byte layouts for wrapped keys, chunk format, and metadata blobs.
- Generate `testvectors/*.json` (known password/salt -> keys, known chunk -> ciphertext).
- **Accept:** spec reviewed by the user; vectors committed.

### M1: Crypto core + CLI
- `core/crypto`: KDF, key wrapping, chunk encrypt/decrypt, metadata blobs, recovery key flow.
- `cli`: `init`, `encrypt <file>`, `decrypt <file>` working on local files only.
- **Accept:** round-trip tests; tamper tests (flip a byte, reorder chunks, truncate -> must fail); passes all test vectors; fuzz tests on the parsers.

### M2: Server
- Auth: signup/login with the derived auth key, sessions per device, device list and revoke.
- Blob API: chunked, resumable upload and download; blobs addressed by opaque IDs.
- Metadata API: store encrypted metadata and the wrapped-key records.
- Rate limiting, request size limits, structured logs with no sensitive data.
- Dockerfile + compose file.
- **Accept:** integration tests against a real server; the server database and storage contain no plaintext (add a test that greps for known plaintext).

### M3: Sync and offline
- Local encrypted SQLite store in `core`.
- Server keeps an append-only change log; each device has a cursor.
- Client queues changes offline and pushes/pulls when connected, with retry and backoff.
- Conflicts: keep both versions (conflict copy); never silently overwrite.
- **Accept:** tests simulating offline edits on two devices, interrupted uploads, and server restarts; no data loss in any scenario.

### M4: Desktop app (Wails)
- Login, klefki browser (folders, tags, favorites), upload/download, preview for PDF/images/text, trash and restore, sync status indicator, app lock and auto-lock.
- Installers/builds for Windows and Linux.
- **Accept:** manual test script in `docs/` passes on both OSes.

### M5: Web client
- TypeScript implementation of the same crypto using libsodium; must pass the shared test vectors.
- PWA with IndexedDB offline store and service worker.
- **Accept:** a file uploaded from desktop opens on web and vice versa; offline edit on web syncs correctly.

### M6: Backup and safety net (do before heavy real use)
- Full encrypted export/import, plus plaintext export.
- Integrity check command that verifies every chunk.
- Documented restore procedure, tested from a clean machine.
- **Accept:** restore from export reproduces the klefki exactly.

### M7: Search and OCR (on-device)
- OCR for scans and images (Tesseract-based, local only).
- Local encrypted full-text index (BM25) built from OCR/text extraction.
- Index is stored locally, encrypted with the klefki key, and synced as a normal blob (rebuildable).
- **Accept:** search finds text inside scanned documents; index contents are never visible server-side.

### M8: Semantic search and classification (approved revisions)
- Entire feature optional and off by default per klefki/device; English-first local
  ONNX model, versioned, with verified on-enable download or local bundle.
- Desktop/CLI build separate encrypted semantic sidecars from the M7 extraction
  pass. Optional web inference consumes synced indexes; no browser OCR/indexing.
- Hybrid search: BM25 + vector similarity, merged with reciprocal rank fusion.
- Classification uses descriptions/examples for Identity, Money, Legal, Health,
  Education and Work, then kNN on explicitly confirmed corrections. Similarity
  cutoff and margin are configurable and tuned on the evaluation set before
  choosing defaults. Categories and fields are reviewable suggestions.
- Metadata extraction covers generic dates and currency amounts with source
  evidence. No expiry-date extraction or reminders in M8.
- Evaluation: at least 50 documents and 100 labelled queries, including
  near-duplicates, paraphrases and exact identifiers/numbers. Report top-3 success
  and MRR for keyword-only, semantic-only and hybrid; hybrid target >=80% top-3.
- **Accept:** generated fixtures pass, followed by sensible natural-language
  results and private acceptance on the user's own labelled document set.
  [Implementation, evidence and pending acceptance](docs/m8-status.md).

### M9: Later
General vault and folder organization, Android and iOS apps, macOS desktop
support, expiry reminders, link sharing (key in URL fragment), emergency access,
and optional on-device LLM. [Planned features and acceptance](ROADMAP.md).
These are future scope; platform stacks and classification models are not yet
selected. The current milestone scope above remains unchanged.

## 6. Working rules for the agent

- Work one milestone at a time, in small commits with clear messages.
- Write tests first for anything in `core/crypto`, sync, and the server's auth.
- **Ask the user before:** changing any file format or key hierarchy, adding a dependency that touches crypto, changing the stack, or deleting data-handling code.
- Never invent cryptographic constructions. If a design question is not answered in `docs/crypto-spec.md`, stop and ask.
- Never log, print, or commit secrets, keys, real documents, or real passwords. Use generated fixtures only.
- Prefer boring and simple. No premature abstractions; no features outside the current milestone.
- After each milestone, update the docs and list known limitations honestly.

## 7. Definition of done (overall)

- A document added on one device appears on the others, including after being offline.
- Server compromise exposes only ciphertext and opaque IDs.
- A full restore from backup works on a clean machine.
- Recovery key flow tested end to end.
- Threat model and crypto spec match the code.

## Approved M0 clarifications (2026-10-09)

The supplied plan above is preserved. During planning the user selected:

- Tamper detection for v1, with rollback, deletion and malicious web-client delivery documented as limitations.
- A derived TLS bearer credential and server-side verifier, not a PAKE.
- A concrete draft spec and public fixtures first, followed by user review before M1.
- `libsodium-wrappers-sumo` in place of `libsodium-wrappers`, because the upstream JavaScript package exposes Argon2id only in sumo.

The user approved the M0 spec and CLI container proposal and authorized M1
on 2026-10-09. Milestone acceptance is tracked in `MILESTONES.md`.
