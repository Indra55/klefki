# M2 status (2026-10-10)

M2 is implemented. The approved v1 cryptographic formats and key hierarchy are
unchanged. The existing M0/M1 files were already uncommitted and were preserved;
no commits were created during M2.

## Delivered

- Standalone `cmd/vault-server`, chi routing, SQLite accounts/sessions/metadata,
  and a disk blob store behind an interface.
- Signup/login with the approved derived credential and constant-time verifier
  comparison; random per-device sessions, hashed tokens, expiry, listing/revocation.
- Immutable, account-isolated chunk uploads/downloads, idempotent retries,
  resumable inventory and atomic encrypted metadata/wrapped-file-key pairs.
- TLS by default, explicit loopback development mode, request/time/rate limits,
  bounded pagination and structured logs without request contents or identifiers.
- Non-root Dockerfile and Compose configuration with TLS and persistent data.

## Verified

- `go test ./...`, `go test -race ./...`, `go vet ./...` passed.
- `go -C testvectors/tools/go test ./...` passed. Root crypto tests also exercise
  the shared vectors. TypeScript was not rerun; its implementation was unchanged.
- Real TLS HTTP listeners (`httptest.NewTLSServer`), actual SQLite files and disk
  storage verify enrollment, duplicate IDs, rejection of the verifier as a login
  credential, missing authentication, expiry, cross-account isolation, device
  revocation and persistence after server close/reopen.
- Round-trip ciphertext download decrypts successfully. A two-chunk file resumes
  after an interrupted request and server restart, then passes whole-stream
  authentication. Conflicting retries fail; concurrent identical disk uploads
  publish exactly once.
- Plaintext/malformed metadata and truncated/oversized chunks are rejected;
  request-size and rate limits, strict JSON fields and TLS-required mode are tested.
- Recursive byte scans of the live SQLite database, WAL/sidecars and blob files,
  plus captured logs, find none of the known synthetic document text, filename,
  tag, password, master/file keys, login credential or returned session token.
  Scans also check hex/base64 encodings. This tests the proper encrypted-client
  path; the server cannot authenticate ciphertext or detect deliberately disguised
  plaintext without client keys.
- `docker compose config --quiet` and `docker build -t cairn-vault:m2 .` passed.
  The built image passed an HTTP/auth startup smoke test as UID 10001, with a
  read-only root filesystem, no capabilities and a temporary data mount. No host
  ports were published. Container TLS deployment with operator certificates was
  not exercised; the integration suite used real TLS listeners.

See [the API guide](server-api.md) for request contracts, operation and limitations.
This is server acceptance, not client sync, browser/device E2E, power-loss testing,
public deployment, a security audit, or M6 backup readiness. Online password reset
and recovery remain unspecified and unavailable. Limits are process-local; storage
quotas and production enrollment controls remain absent.
