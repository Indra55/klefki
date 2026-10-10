# Server API (M2 baseline and M3 extension)

Run `go run ./cmd/klefki-server -dev-http` for loopback-only development on
127.0.0.1:8080. Data defaults to `./server-data`. For normal use, supply
`-tls-cert PATH -tls-key PATH` and optionally `-listen HOST:PORT -data PATH`.
Clients must validate the server certificate. HTTP is rejected unless explicitly
allowed for development; the executable restricts that mode to literal loopback
addresses. Forwarded headers are not trusted. TLS termination at a proxy is not
configured by this milestone.

The server uses chi, SQLite (WAL) and a disk-backed `BlobStore` interface. The data
directory must be private and controlled exclusively by the server operator; do
not put symlinks or unrelated files there. One server process per data directory.

## Wire conventions

All endpoints are under `/v1`. IDs are nonzero, lowercase 32-character hex strings.
JSON byte fields use standard padded base64. Chunk bodies are binary v1 records.
These transport encodings do not alter the approved cryptographic formats.
Responses are `Cache-Control: no-store`. Errors contain only HTTP status text.

The client creates the account and all encrypted records locally using
`core/crypto`. Signup sends the **credential**, never the password, auth key,
KEK, master key, recovery key or server verifier. The server derives the verifier
using the approved v1 function. It cannot enforce a plaintext password policy or
authenticate ciphertext without the client's keys.

| Method and path | Request / response |
|---|---|
| `POST /signup` | JSON: `account`, `descriptor`, `password_wrapper`, `recovery_wrapper`, `credential`. Creates account and device session atomically; 201, or 409 if ID exists. |
| `GET /accounts/{account}` | Public unlock records: `account`, `descriptor`, `password_wrapper`, `recovery_wrapper`; 404 if absent. No verifier. |
| `POST /login` | JSON: `account`, `credential`. Returns a new device session; 200 or generic 401 for absent account/incorrect credential. |
| `GET /devices` | Active device sessions: array of `{id, expires}`. |
| `DELETE /devices/{device}` | Revokes an owned device, including the current one; 204 or 404. |
| `PUT /blobs/{file_id}/chunks/{index}` | Complete binary kind-5 record, bound to account/file/index; 201, identical retry 204, different existing bytes 409. |
| `GET /blobs/{file_id}/chunks/{index}` | Exact stored ciphertext record; 200 or 404. |
| `GET /blobs/{file_id}/chunks` | Ascending array of stored chunk indices, including partially uploaded files. |
| `PUT /metadata/{metadata_id}` | JSON: `metadata` (kind 4), optional `file_key` (kind 3). Atomic immutable pair; 201, identical retry 204, conflicting bytes 409. |
| `GET /metadata/{metadata_id}` | Same record pair; 200 or 404. |
| `GET /metadata` | Sorted array of stored metadata IDs. |

All endpoints except signup, login and account discovery require
`Authorization: Bearer TOKEN`. Signup/login return `{token, device, expires}`;
token is 32 random bytes encoded as hex, device is an opaque random ID, and expires
is Unix seconds. Only SHA-256 token hashes persist. Sessions expire after 30 days;
login creates another device entry. Device names are not stored. Revocation blocks
subsequent requests; a request already authenticated can finish. It does not erase
offline copies. No cookie authentication or permissive CORS is enabled.

Lists return at most 1000 entries. Pass `?after=LAST_VALUE` to continue (device ID,
metadata ID, or decimal chunk index respectively), until an empty page. Listing
is not a snapshot or a sync cursor. Concurrent writes may require another scan.
Opaque IDs, rather than email/usernames, avoid a human-readable account directory;
clients must retain their account ID. Discovery reveals whether a *known* ID exists.

## Upload and download protocol

Encrypt once with a fresh revision key on the client. Upload each complete chunk
independently; retain the same ciphertext for retries. List uploaded indices after
an interruption and resend missing chunks. A partial request never publishes a
chunk. Disk publication uses a private temporary file, fsync and exclusive hard
link, so concurrent retries cannot replace accepted bytes. Normal errors clean up
temporary files; a process crash can leave an ignored `.upload-*` ciphertext file.
Hard-link support is required. This is chunk-level resume, not byte-range PATCH.

Download individual chunks and authenticate the complete stream on the client,
using decrypted metadata size/count. The server validates public envelope shape,
not AEAD tags or whole-file completeness. Upload the metadata/key pair after all
chunks. A pair is atomic, but does not establish a transaction with blob storage.
Folder metadata omits `file_key`. The server cannot inspect encrypted type, name,
size or file-key association; clients must verify those. These are the legacy M2
immutable endpoints. M3 adds revisions, a change log,
device cursors and conflict-preserving sync through separate endpoints. See
[the sync protocol](sync-protocol.md); M3 metadata edits reuse the logical object
ID and receive fresh revision/file IDs. Deletion remains unimplemented.

## Limits and logging

- Signup/login bodies: 8192 bytes. Metadata JSON: 1,500,000 bytes (enough for the
  maximum base64-encoded v1 metadata plus wrapper). Chunk bodies: 4,194,391 bytes.
  Oversized bodies return 413; malformed envelopes/JSON return 400.
- Executable: 8 KiB header limit, 5-second header timeout, 30-second read/write
  timeouts and 60-second idle timeout.
- Token-bucket rate limiting: 120 requests/minute with burst 120 per peer IP,
  independently for public authentication/discovery and authenticated data APIs.
  Login also limits each account ID. Buckets are bounded to 10,000 entries and
  held in memory; restarting resets them. 429 indicates throttling. NAT peers
  share budgets; a proxy would too, since forwarding headers are ignored.
- Request logs contain only fixed event text, status and duration. No URL, query,
  peer address, headers, request/response body, credentials or record contents.
  Initialization/listener errors use fixed messages. Low-level HTTP/TLS diagnostic
  logging is suppressed to avoid peer-controlled details.

No storage/account quotas, distributed throttling, automatic expired-session or
orphan-blob cleanup, password changes, remote recovery, or production enrollment
controls are implemented. Restrict network exposure to trusted users during M2.
The server cannot prove a malicious client submitted ciphertext rather than
plaintext disguised as a valid envelope. The no-plaintext acceptance test covers
correct clients using real v1 encryption, not arbitrary uploads or a security audit.

## Docker

`docker build -t klefki:m3 .` builds a non-root image. Compose mounts a named
volume at `/data` and a read-only `./tls` directory. Provision `tls/server.crt` and
`tls/server.key` with a certificate trusted by your clients; UID 10001 must be able
to read the key. TLS files are excluded from Git and the image build context.
Then run `docker compose up --build -d`. The default published port is loopback
8443. The container has a read-only root filesystem and no Linux capabilities.
Keep the SQLite database, WAL and blob tree together; consistent backup/restore
procedures remain M6 work.
