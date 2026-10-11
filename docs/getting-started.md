# Getting started

Klefki runs locally as a desktop app, a web app, or a CLI. Sync is optional;
you can add and read documents offline.

## Web app

Requires Go 1.26+ and Node 22.18+. From the repository root:

```sh
npm --prefix web ci --ignore-scripts
npm --prefix web run build
go run ./cmd/klefki-server -dev-http -web web/dist
```

Open <http://127.0.0.1:8080>. Create an account, save the recovery code, and add
a document. Register the account in **Klefki settings** to enable sync.
For an account already used on desktop, open its exported encrypted account
file and unlock with the same password.

The HTTP command is for local development. Use HTTPS for normal operation.
See [web setup and offline behavior](m5-status.md) and [server configuration](server-api.md)
for deployment, TLS, and Docker.

For optional [passkey unlock](passkey-web.md), open an HTTPS domain or
`http://localhost:8080`, unlock with your password, then select **Enable passkey
unlock** in **Klefki settings**. Fingerprint, face, or PIN verification depends
on the device and PRF-capable credential provider. IP-address URLs cannot use
WebAuthn; changing origins does not migrate browser storage.

## Desktop app

The desktop app targets Linux and Windows and uses Wails v2. It needs Go, Node,
and the [Wails platform prerequisites](https://wails.io/docs/gettingstarted/installation).

On Linux, with GTK 3 and WebKitGTK 4.1 development packages installed, run from
the repository root:

```sh
make linux-demo
```

This installs frontend dependencies when needed, rebuilds the frontend and
Linux executable with native ONNX support, and launches Klefki. It uses your
existing app settings and library. Close any running Klefki window first so
the new binary opens. The local model is downloaded only when enabled in Settings.
Use `make linux-build` to build without launching.

To install a persistent Linux app with an app-drawer entry, use Python 3 and run
as your normal desktop user (without `sudo`):

```sh
make linux-install
```

Open **Klefki** from the app drawer. The installed executable is
`~/.local/bin/klefki-desktop`, independent of the repository and its build output.
Your account, encrypted document database, and settings remain in
`${XDG_CONFIG_HOME:-~/.config}/Klefki`; installation does not change them.
The icon and launcher use `${XDG_DATA_HOME:-~/.local/share}`.

After updating the source, rebuild and replace the installed app with:

```sh
make linux-update
```

A failed build leaves the installed executable intact. Installation replaces
the executable atomically and retains the previous executable when it changes.
Close and reopen Klefki to use the new version. To switch back:

```sh
make linux-rollback
```

Rollback swaps the installed and previous executables; it does not restore data
or reverse database migrations. Keep [encrypted backups](backup-restore.md)
for document recovery. The installed app still requires the Linux GTK 3 and
WebKitGTK 4.1 runtime libraries.

For a Wails build on either platform:

```sh
cd desktop
wails build
```

On Linux with WebKitGTK 4.1, use `wails build -tags webkit2_41`.
The executable is written to `desktop/build/bin/klefki-desktop`
(`klefki-desktop.exe` on Windows).

Create a private space, save the recovery code, and add files. Open **Settings**
from the top bar to choose a theme, document spacing, default sort order, and
inactivity lock time. **Sync & devices** connects a sync server or exports the
encrypted account file for another device; **Search** manages local indexing
and optional semantic search. These preferences are saved on this device.
Recovery setup asks for a folder, then confirms the full destination before
saving a plaintext `klefki-recovery.txt`. An existing file is never overwritten;
canceling either dialog stops account creation.
The [desktop walkthrough](m4-acceptance.md) covers folders, previews, trash,
locking, and sync. Linux users can also enable [fingerprint unlock](fingerprint-linux.md).

## CLI

Requires Go 1.26+. Build from the repository root:

```sh
go build -o klefki ./cli
./klefki help
```

Initialize an account and save its recovery code to a private location **outside
this repository** (replace the recovery path with an existing private directory):

```sh
./klefki init --recovery-out /private/location/klefki-recovery.txt
./klefki encrypt -o document.klefki /path/to/document.pdf
./klefki decrypt -o restored.pdf document.klefki
```

Passwords are prompted with echo disabled. The default account file is
`.klefki/account.v1`; use `--klefki PATH` for another account. For automation, supply
`--password-file PATH` instead of a terminal password. Passwords and recovery
codes are never accepted as argument values or printed to stdout.

Recover a document without the password:

```sh
./klefki decrypt --recovery-file /private/location/klefki-recovery.txt -o recovered.pdf document.klefki
```

Flags precede the input file. Encryption without `-o` creates an opaque
`<file-id>.klefki` filename beside the source. Decryption requires `-o` and never
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
go run ./cmd/klefki-server -dev-http
```

Normal operation requires TLS. See the [server guide](server-api.md) for
TLS, Docker and API usage.

## Offline sync

After initializing an account, register it once and add files locally:

```sh
./klefki register --server http://127.0.0.1:8080 --dev-http
./klefki add /path/to/document.pdf
./klefki sync --server http://127.0.0.1:8080 --dev-http
./klefki list
```

Use HTTPS for normal operation. `add` works offline and prints object/revision IDs.
Edit with `add --object ID --base REVISION FILE`; export a selected copy with
`export -o PATH REVISION`. Conflicting edits remain separate copies. See the
[sync guide](sync-protocol.md) for multiple devices, storage and retry details.

## Search and recovery

Desktop and CLI can [index document text and scans locally](m7-status.md).
[Semantic search and suggestions](m8-status.md) are optional and require a local
model setup. Web reads synced indexes; it does not perform OCR itself.

Keep the recovery code separately from your documents. The
[backup and restore guide](backup-restore.md) explains encrypted backups,
plaintext export, integrity checks, and recovery.

[Documentation index](README.md)
