# M1 — Crypto core and local CLI

Implementation and M1 acceptance checks passed on 2026-10-10. M2 has not started.
The work is now committed as part of reconstructed milestone history.
See [the checklist](../MILESTONES.md) for current milestone acceptance.

## Delivered

- `core/crypto`: Argon2id, separated KEK/auth keys, auth credential/verifier,
  random keys/IDs/nonces, authenticated key wrappers, metadata records, chunk
  streams, canonical recovery codes, password unlock and local recovery.
- Bounded record/descriptor/metadata parsing, exact context checks, complete-file
  validation, duplicate-field and invalid-Unicode rejection, and best-effort key
  wiping. Unknown metadata fields are ignored; known field names are exact and
  cannot be overridden by case aliases.
- `cli`: local `init`, `encrypt FILE`, and `decrypt FILE`, password or recovery-file
  unlock, versioned containers, private recovery-code output, and private staging
  with non-overwriting publication. A failure does not publish partial plaintext.
- Core and CLI tests were written and observed failing before implementation.
  The approved container layouts are documented in the crypto spec. Existing
  encrypted record formats and key hierarchy were preserved.
- `golang.org/x/term` supplies hidden terminal input; it is not a new crypto
  primitive implementation. Go crypto remains the previously selected library.

## Verified

| Check | Evidence |
|---|---|
| `go test ./...` | Passed core and CLI suites |
| Shared fixtures through production core | Three KDF cases, all seven complete records, six boundary stream digests, recovery encoding and all 25 rejections passed |
| Tamper handling | Modified records/AAD, wrong identity/key, swapped/missing/truncated/appended chunks and mixed revisions rejected |
| CLI safety | Multi-chunk round trip, recovery, wrong password, no overwrite, no plaintext filename/content in ciphertext, no secret output, and staging cleanup passed |
| `go test -race ./...` | Passed |
| `go vet ./...` | Passed |
| Seven parser fuzz targets | Passed with 3-second budgets and two workers per target; approximately 1.51 million combined executions |
| Built Linux binary | Separate process smoke passed init, 5,120,000-byte encrypt/decrypt, recovery and 0600 output modes |
| Windows amd64 cross-build | Passed; not executed on Windows |
| M0 Go and TypeScript verifiers | Passed; npm DNS blocker resolved |

The fuzz targets are record, metadata, recovery-code, descriptor, chunk-stream,
account-container and file-container parsers. Reproduce from the repo root:

```sh
for target in FuzzRecordParser FuzzMetadataParser FuzzRecoveryParser FuzzDescriptorParser FuzzStreamParser; do
  go test ./core/crypto -run '^$' -fuzz "^${target}$" -fuzztime=3s -parallel=2 || break
done
for target in FuzzAccountContainer FuzzFileContainer; do
  go test ./cli -run '^$' -fuzz "^${target}$" -fuzztime=3s -parallel=2 || break
done
GOOS=windows GOARCH=amd64 go build -o /tmp/klefki-m1.exe ./cli
```

Local verification used `GOCACHE=/tmp/klefki-go-cache GOPROXY=off GOSUMDB=off`
with cached pinned modules and `go.sum`. It did not perform a fresh online Go
checksum-database lookup. Ordinary connected users should retain Go defaults.

## Known limitations

- No security audit; short fuzz runs are smoke coverage, not exhaustive proof.
- Windows runtime, ACLs and interactive hidden-input behavior have not been
  manually tested. Terminal code was compiled; automated CLI tests used secret
  files. Windows and Linux desktop acceptance belongs to M4.
- Decryption writes authenticated chunks into a private staging file before
  validating final EOF. Ordinary errors remove it; process/OS crashes can leave
  private staging files. Secure deletion, crash-proof cleanup, directory fsync,
  Windows ACL configuration and protection against a compromised endpoint are
  not claimed. Filesystems without hard-link support fail closed.
- Source length changes during encryption fail, but concurrent same-length
  modifications are not detected as a consistent snapshot. Use stable inputs.
- Go strings/runtime copies cannot be reliably erased. Password files, recovery
  files and plaintext exports need host-level protection. Enrollment rejects
  empty passwords; it does not yet impose a strength policy.
- Account/recovery output consists of two exclusive file publications, not a
  cross-file filesystem transaction. Back up both account state and ciphertext;
  remote recovery authorization and password-change UI are later work.
- No rollback protection, local database, server, sync, desktop/PWA, OCR or search.
  No backup/restore claim until M6. Do not use public fixture keys/nonces for data.
