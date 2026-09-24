# Vault milestones

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

This checkpoint is part of reconstructed milestone history. Recorded implementation
and verification dates in the evidence documents remain unchanged.
