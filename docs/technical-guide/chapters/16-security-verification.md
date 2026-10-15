# Security boundaries, testing, and deployment mechanics

The strongest way to understand the project is to identify its invariants, then ask which checks establish each one. A build proves the code compiles; it does not prove a fingerprint reader works, a restore preserves your documents, or a hosted server runs the same revision.

## Threat boundaries

| Event | Intended defense | Remaining boundary |
| --- | --- | --- |
| Server disk theft | Encrypted documents, metadata, keys and sidecars | Password guessing and visible structural metadata |
| Altered chunk | AEAD context plus stream completeness | Whole valid historical replay remains possible |
| Interrupted local import | Atomic store transaction | Hardware/crash behavior is wider than ordinary error tests |
| Lost upload response | Immutable idempotent retry | Server availability and storage loss remain possible |
| Concurrent offline edits | Preserve multiple heads | No automatic semantic merge or authenticated global graph |
| Locked local store theft | Wrapped keys and ciphertext records | OS memory/swap/keyring and weak passwords |
| Malicious browser origin | No independent protection from supplied malicious code | Origin controls executable client code |
| Logical permanent deletion | Current object fences and cleanup | Offline copies, backups and forensic erasure |

The server can observe opaque IDs, size, timing, sessions, and some revision/delivery relationships. Encryption does not hide every column or traffic pattern. TLS secures transport; record AEAD secures content/context; transactions secure local publication boundaries. None replaces the others.

## Cross-language test vectors

Go and TypeScript need exactly the same KDF output, envelope bytes, integer encoding, nonce handling, and rejection behavior. `testvectors` supplies public synthetic secrets and fixtures for both implementations. Fixed test keys/nonces are useful there but must never become production defaults.

The vectors act like a shared ruler: two independent implementations measure the same record and must agree. Passing vectors establishes those cases, not an audit of every parser or runtime integration.

## Failure-oriented tests

Crypto tests cover tampering, malformed metadata, chunks, descriptor/recovery rules, and fuzz parsers. Sync/server tests exercise conflicts, resumability, retries, durable cursors, account scope, relay cleanup, batching, concurrency, and status. Backup tests cover snapshot/restore checking. Search tests use controlled extraction fixtures and labelled retrieval/semantic evaluations.

Good failure tests target invariants. For example, corrupting the last chunk should fail before a file becomes visible or a cursor advances. Repeating an identical revision publication should not append a duplicate event. Concurrent edits should preserve both heads. A structural SQLite check should not substitute for authenticating ciphertext.

## Validation commands and their scope

| Command | What it can establish |
| --- | --- |
| `go test ./...` | Root-module unit/integration behavior under those tests |
| `go test -race ./...` | Exercised Go data races, not all possible schedules |
| `go vet ./...` | Static suspicious-pattern checks |
| `go -C testvectors/tools/go test ./...` | Separate Go vector tooling |
| `npm --prefix web test` | Browser crypto fixtures through the script's runtime |
| `npm --prefix web run test:e2e -- ...` | Selected automated browser flows with test server |
| `make linux-build` | Frontend assets plus native executable compile |
| Native/device walkthrough | Actual dialogs, keyring, reader, and OS behavior tested there |

The desktop is a separate Go module, so root `go test ./...` does not automatically test everything under `desktop`. Native compilation/acceptance and browser fixture bridges are separate. ONNX-enabled tests also require the chosen build tags and model/runtime assets.

This guide's validation covers the book/reader. Application checks, hosted deployment, and physical-device acceptance require their own evidence; this documentation review does not establish them.

## Deployment mechanics

The base `Dockerfile` builds a Go server and runs it as a nonroot user with a data directory and TLS listener configuration. The web-serving deployment has separate packaging/model requirements; inspect its actual Docker/hosting files rather than assuming the base server image includes all client assets.

`compose.yaml` attaches the named `klefki-data` volume at `/data`, mounts TLS files read-only, binds the listener to host loopback, drops capabilities, and uses a read-only container root filesystem. Its writable data volume is where database and blob persistence belong.

`Dockerfile.render` instead uses a Node build stage to download verified model assets and compile the web client, a Go stage for the server, and a final image containing `/web` plus the server executable. Its command selects the hosting-provided port, serves `/web`, enables proxy-HTTP mode, and sets the relay queue limit from an environment variable. `render.yaml` selects that image and `/healthz`. These are source configuration facts, not a live deployment-state check.

Persistent mode requires persistent database/blob storage. An ephemeral host can lose both on restart. Temporary relay can be reseeded by linked devices retaining history, but it is not an automatic server backup service.

When adding protocol extensions, deploy a compatible server before clients that need them. The relay optimization retained older endpoints and client fallbacks. Current permanent deletion instead checks capability before allowing a bound store to rely on deletion, because an old server ignoring deletion fences could reintroduce records.

## What remains outside proof

No external cryptographic audit is recorded. Anti-rollback/fork detection, shared libraries, mobile native apps, and broader future features are not implied by current encrypted sync. Read milestone evidence and the roadmap separately from implementation source.

## Follow the source

- [Threat model](../../threat-model.md), [development checks](../../development.md)
- [Crypto tests](../../../core/crypto/crypto_test.go), [sync tests](../../../core/sync/sync_test.go)
- [Server tests](../../../server/server_test.go), [restore tests](../../../cli/backup_test.go)
- [Shared vectors](../../../testvectors/README.md)
- [Docker server image](../../../Dockerfile), [Compose](../../../compose.yaml), [web-serving image](../../../Dockerfile.render), [hosting configuration](../../../render.yaml), [milestones](../../../MILESTONES.md)
