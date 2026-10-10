# Development and verification

The shared core, server, and CLI use Go 1.26+. The web app requires Node 22.18+;
the desktop frontend uses React and Wails. Run commands from the repository root
unless a different working directory is shown.

## Core and shared vectors

```sh
go test ./...
go test -race ./...
go vet ./...
go -C testvectors/tools/go test ./...
npm --prefix testvectors/tools/ts ci --ignore-scripts --no-audit --no-fund
npm --prefix testvectors/tools/ts test
```

See [M1 status](m1-status.md) for fuzz commands and exact evidence boundaries.

## Frontends

```sh
npm --prefix desktop/frontend ci --ignore-scripts --no-audit --no-fund
npm --prefix desktop/frontend test
npm --prefix desktop/frontend run build
npm --prefix web ci --ignore-scripts --no-audit --no-fund
npm --prefix web test
npm --prefix web run build
```

For browser acceptance against a local Go server:

```sh
npm --prefix web run test:e2e -- e2e/klefki.spec.ts
```

The desktop frontend browser tests use a fixture bridge:

```sh
cd desktop/frontend
PW_DISABLE_TS_ESM=1 node node_modules/@playwright/test/cli.js test
```

Browser tests require Chromium installed through Playwright and permission to
open local ports. The desktop tests exercise the frontend; native dialogs and
physical fingerprint scans have separate acceptance steps.

## Optional checks

- [Desktop builds and native acceptance](m4-status.md)
- [Clean-container backup/restore acceptance](backup-restore.md)
- [Local OCR prerequisites and acceptance](m7-status.md)
- [Native ONNX builds, model setup, and search evaluation](m8-status.md)
- [Screenshot sources](screenshots/README.md)

[Milestones](../MILESTONES.md) record acceptance status. The implementation
records linked from the [documentation index](README.md) distinguish automated
checks, user-reported native results, and remaining limitations.
