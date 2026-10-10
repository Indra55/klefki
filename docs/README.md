# Klefki documentation

## Using Klefki

| Guide | Covers |
| --- | --- |
| [Getting started](getting-started.md) | Web, desktop, CLI, and your first account |
| [Desktop walkthrough](m4-acceptance.md) | Organizing, previewing, locking, and syncing documents |
| [Web guide](m5-status.md) | PWA setup, offline use, and connecting an existing account |
| [Backup and restore](backup-restore.md) | Encrypted backups, recovery, exports, and integrity checks |
| [Search and OCR](m7-status.md) | Local text extraction, prerequisites, and keyword search |
| [Semantic search](m8-status.md) | Optional models, hybrid search, and reviewable suggestions |
| [Linux fingerprint unlock](fingerprint-linux.md) | Reader setup, keyring behavior, and security limits |

## Running a server and understanding the design

- [Server API and deployment](server-api.md): TLS, Docker, authentication, and blob storage.
- [Sync protocol](sync-protocol.md): offline queues, retries, device cursors, and conflict copies.
- [Threat model](threat-model.md): protections, assumptions, and limits.
- [Cryptographic format](crypto-spec.md): key hierarchy and exact v1 byte layouts.
- [Shared test vectors](../testvectors/README.md): Go and TypeScript interoperability fixtures.

## Development

[Builds and verification](development.md) · [Project plan](../PLAN.md) · [Milestones](../MILESTONES.md) · [Achievements](../achievements.md)

Implementation records preserve the evidence and remaining limitations for each milestone:

| Milestone | Record |
| --- | --- |
| M0: Specification | [Status](m0-status.md) |
| M1: Crypto and CLI | [Status](m1-status.md) |
| M2: Ciphertext server | [Status](m2-status.md) |
| M3: Offline sync | [Status](m3-status.md) |
| M4: Desktop | [Status](m4-status.md) |
| M5: Web | [Status](m5-status.md) |
| M6: Backup and recovery | [Status](m6-status.md) |
| M7: Search and OCR | [Status](m7-status.md) |
| M8: Semantic search | [Status](m8-status.md) · [Evaluation](m8-evaluation.json) |

[Screenshot sources](screenshots/README.md) · [Project overview](../README.md)
