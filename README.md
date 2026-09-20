# Vault

Personal, offline-first encrypted document storage. A Go crypto core, CLI,
ciphertext server, encrypted local store, and offline sync with conflict copies
support the Wails desktop app for Linux and Windows and a TypeScript web PWA.
Local OCR and encrypted keyword search are available on desktop and CLI.

- [Full specification](PLAN.md)
- [Milestone checklist](MILESTONES.md)
- [Threat model](docs/threat-model.md) and [crypto format](docs/crypto-spec.md)
- [M1 evidence and limitations](docs/m1-status.md)
- [Server API and Docker setup](docs/server-api.md)
- [M2 evidence and limitations](docs/m2-status.md)
- [Offline sync and CLI guide](docs/sync-protocol.md)
- [M3 evidence and limitations](docs/m3-status.md)
- [M4 desktop evidence](docs/m4-status.md) and [acceptance walkthrough](docs/m4-acceptance.md)
- [M5 web setup, verification and limitations](docs/m5-status.md)
- [Backup and restore guide](docs/backup-restore.md) and [M6 evidence](docs/m6-status.md)
- [Local search and OCR: setup and M7 evidence](docs/m7-status.md)
- [Linux fingerprint unlock](docs/fingerprint-linux.md)
- [Shared test vectors](testvectors/README.md)

## Local CLI

Requires Go 1.26+. Build from the repository root:

```sh
go build -o vault ./cli
./vault help
```

Initialize an account and save its recovery code to a private location **outside
this repository** (replace the recovery path with an existing private directory):

```sh
./vault init --recovery-out /private/location/vault-recovery.txt
./vault encrypt -o document.vault /path/to/document.pdf
./vault decrypt -o restored.pdf document.vault
```

Passwords are prompted with echo disabled. The default account file is
`.vault/account.v1`; use `--vault PATH` for another account. For automation, supply
`--password-file PATH` instead of a terminal password. Passwords and recovery
codes are never accepted as argument values or printed to stdout.

Recover a document without the password:

```sh
./vault decrypt --recovery-file /private/location/vault-recovery.txt -o recovered.pdf document.vault
```

Flags precede the input file. Encryption without `-o` creates an opaque
`<file-id>.vault` filename beside the source. Decryption requires `-o` and never
uses the encrypted filename as a filesystem path. Existing output files are
never replaced. Source files remain intact. Recovery requires the account file,
the encrypted file, and the recovery code; this is not server account reset.

Outputs use private staging, complete authentication, and exclusive publication.
The filesystem must support hard links (for example, local Linux filesystems or
NTFS). Linux file modes are tested; Windows has only been cross-compiled.
Plaintext outputs and recovery-code files are intentionally outside encryption.

## Server

For local development:

```sh
go run ./cmd/vault-server -dev-http
```

Normal operation requires TLS. See the [server guide](docs/server-api.md) for
TLS, Docker and API usage.
