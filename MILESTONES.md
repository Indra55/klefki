# Klefki milestones

`PLAN.md` is the full specification. Check a milestone only after its acceptance
criteria pass; link evidence and limitations here. Implementation/test completion is distinct from committing the work.

- [x] **M0 — Spec and test vectors.** Spec reviewed; Go and TypeScript vectors
  pass; specification and vectors are committed. [Evidence](docs/m0-status.md).
- [x] **M1 — Crypto core + CLI.** Local init/encrypt/decrypt and recovery;
  round trips, all shared vectors, tamper tests and seven parser fuzz targets
  passed. [Evidence and limitations](docs/m1-status.md).
- [x] **M2 — Server.** Auth/devices, ciphertext APIs, limits, Docker and real-server
  TLS integration tests. [Evidence and limitations](docs/m2-status.md).
- [x] **M3 — Sync and offline.** Encrypted local store, persistent queue, change log,
  device cursors and conflict copies; offline/interruption/restart tests passed.
  [Evidence and limitations](docs/m3-status.md).
- [x] **M4 — Desktop.** Wails app and Linux/Windows builds; native desktop
  acceptance confirmed by the user on both OSes. Linux physical fingerprint
  unlock, contact feedback, and restart also user-verified.
  [Acceptance walkthrough](docs/m4-acceptance.md) · [Evidence and limitations](docs/m4-status.md).
- [x] **M5 — Web.** Compatible TypeScript crypto, PWA/IndexedDB, and six Chromium
  acceptance cases passed, including desktop service cross-client transfer and
  offline editing/reload. [Evidence and limitations](docs/m5-status.md).
- [x] **M6 — Backup and safety net.** Full encrypted backup/restore, plaintext export,
  and every-chunk integrity checks. Password and recovery restores reproduced the
  klefki exactly in a clean Linux container.
  [Restore procedure](docs/backup-restore.md) · [Evidence and limitations](docs/m6-status.md).
- [x] **M7 — Search and OCR.** Local Tesseract/PDF extraction, encrypted BM25 index,
  desktop and CLI search. Real scanned-document search and encrypted index sync
  acceptance passed. [Use, evidence and limitations](docs/m7-status.md).
- [ ] **M8 — Semantic search and classification.** Optional local ONNX model,
  hybrid search and reviewable analysis implemented. Public 60-document /
  120-query evaluation passed the hybrid target; private-document acceptance
  remains pending. [Evidence, use and limitations](docs/m8-status.md).
- [ ] **M9 — Later.** Android, reminders, sharing, emergency access and optional local LLM; scope not yet scheduled.


M0–M8 implementation is recorded in 36 reconstructed milestone commits.
Commit dates were assigned retrospectively across September 10–October 10, 2026;
recorded approval, implementation and validation dates remain unchanged.
M8 private-document acceptance remains pending.
