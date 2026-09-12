# Public synthetic v1 vectors

Every password, key, recovery code, plaintext and nonce here is intentionally
public synthetic test data. Never use these values or the deterministic nonce
recipe with real documents. Tools live here to keep them separate from future
production `core` and `web` code.

## Reproduce and verify

Run from the repository root, using Go 1.26+ and Node 22.18+:

```sh
go -C testvectors/tools/go test ./...
go -C testvectors/tools/go run .
npm --prefix testvectors/tools/ts install --ignore-scripts --no-audit --no-fund
npm --prefix testvectors/tools/ts test
```

Go and TypeScript independently invoke their libraries and assemble the record
bytes. TypeScript runs directly using Node's built-in type stripping, without a
TypeScript compiler dependency. Its Node execution does not establish browser
compatibility or memory performance.

Explicit regeneration (never run automatically during verification):

```sh
go -C testvectors/tools/go run . -write
```

Review regenerated JSON against the spec; do not regenerate merely to make a
failing test pass. Cross-language verification must pass before accepting changed
vectors. SHA-256 digests for large records keep the fixture compact; small
records contain their complete ciphertext and tag.

In a restricted workspace with cached Go dependencies:

```sh
GOCACHE=/tmp/vault-go-cache GOPROXY=off GOSUMDB=off go -C testvectors/tools/go test ./...
```

The offline command still checks modules against checked-in `go.sum`; it cannot
perform a fresh public checksum-database lookup. Normal connected installs
should retain Go's default checksum-database settings.

## Fixture contract

`vectors-v1.json` contains:

- Three KDF cases (ASCII, Unicode including a combining mark, and empty input),
  exact descriptor bytes, 64-byte output, split keys, credential and verifier.
- Seven complete encrypted records: password/recovery master wrappers, file-key
  wrapper, Unicode metadata, and empty/binary/Unicode final chunks.
- A recovery key/code pair and local unwrap-through-document checks.
- Six stream recipes for lengths 0, 1, 4 MiB − 1, 4 MiB, 4 MiB + 1 and 8 MiB.
- Twenty-five rejection cases covering context, ciphertext and framing changes,
  incomplete/reordered/mixed-revision streams, and unsupported KDF descriptors.

Binary fields are lowercase hex. For stream length `L`, plaintext byte at absolute
position `j` is `j % 251`. Account ID is bytes 0 through 15, file ID is bytes 16
through 31, and the file key is bytes 96 through 127. The chunk ID is the ASCII
string `stream-L-i`, where L and i use unpadded decimal. Its fixture nonce is the
first 24 bytes of `SHA256(UTF8("vault-fixture/" + chunk_id))`. Use the spec's chunk
layout, final flag and count rules; hash each entire encoded record with SHA-256.
The JSON stores those record digests in order. This deterministic recipe is
strictly for fixtures, not a production encryption algorithm.

Each negative case names a positive base and one mutation. Record mutations
change the indicated field by the fixed operation in both runners: ciphertext
mutation flips the final tag byte, nonce mutation flips byte 47 of the chunk,
index/final/length mutations change their header bytes, and context/key mutations
change the expected decryption inputs. `wrong-extra` changes the last descriptor
salt byte supplied as AAD. Stream cases mutate the 4 MiB + 1 stream; mixed revision
re-encrypts its last chunk using bytes 128 through 159 as a different file key.
KDF cases alter memory, time, lanes, version or descriptor length and must reject
before allocating Argon2 memory.

Fixed package versions are in the tool manifests. The npm transitive
`libsodium-sumo` dependency is also pinned via an override. The lockfile was generated after the user restored npm access. Both the Go
and real TypeScript verifiers pass. These Node results do not establish browser
compatibility.
