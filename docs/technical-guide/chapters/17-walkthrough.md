# One document end to end, source routes, and glossary

This chapter ties the mechanisms together. The example uses symbolic IDs for clarity; real IDs are random nonzero lowercase 32-hex values, and real ciphertext/nonces are not deterministic.

## Follow the 9 MiB bill

**Create the archive.** Client A creates account A, random master key M, recovery key R, and KDF descriptor D. Password-derived KEK seals M; R seals another copy of M. The 260-byte account file can travel to another trusted client because it contains wrappers rather than M in plaintext.

**Import.** `electricity-bill.pdf` gets object O, revision V0, file F0, and key K0. Metadata says type=file, name, parent=the Bills object, tags, size=9,437,184, and count=3. Metadata is encrypted under M with O as context. K0 is wrapped under M with F0 as context. Three chunk records use K0 and authenticate account A, F0, indices 0–2, lengths, and final state.

**Commit offline.** The store saves these records and `pending=1` in one transaction. A failed network connection cannot undo this completed import. The source PDF still exists independently; the encrypted database does not become a filesystem move operation.

**Register and push.** Client A derives a server credential from the auth half of the KDF output and obtains a scoped session. It inventories F0 chunks, uploads the missing ones, and publishes V0. It clears pending only after publication succeeds.

**Receive.** Client B starts from its local cursor and downloads V0. It unlocks the same M from the copied encrypted account. It verifies metadata under O, unwraps K0 under F0, verifies all three chunk records, and commits records plus cursor. Only then does B acknowledge receipt. In relay mode, cleanup can use durable delivery requirements; persistent mode retains server content.

**Rename without re-encrypting bytes.** B creates V1 based on V0, changing only the name. F0, K0's wrapper, and content chunks stay associated with the unchanged content. A fresh metadata nonce protects the new metadata record.

**Create a conflict.** A was offline and creates V2 from V0. After replication, V1 and V2 are heads of O. Upload order does not decide the winner. They are two visible editions with the original authenticated metadata of each branch.

**Index.** Desktop/CLI select current visible heads, export authenticated bytes, and use Poppler/Tesseract for the PDF. The word `MTR9021` enters an encrypted keyword sidecar. Optional local BGE inference turns text windows into 384-dimensional vectors in a semantic sidecar. These sidecars travel through ordinary encryption/sync.

**Query.** “MTR9021 electricity” gets lexical evidence and BM25-style ranking. “The charge for powering my house” may get semantic candidates if the enabled model/index supports it. Hybrid mode filters weak semantic evidence and fuses ranks. Neither route needs server plaintext search.

**Back up.** The CLI snapshots a selected synchronized local store with `VACUUM INTO`, verifies all retained revisions/chunks, and saves encrypted account/database files. Recovery restores access locally if the password is lost and R plus wrappers/ciphertext survive.

**Trash/delete.** Trash first changes encrypted parent/timestamp metadata so restoration is possible. Permanent deletion later records an object fence, checks expected heads, removes local object history/chunks, and propagates the fence. Older independent backups still retain whatever they contained.

## Practical source-reading routes

| Question | Start here | Then follow |
| --- | --- | --- |
| How does unlock produce usable keys? | [Service.Unlock](../../../core/desktop/service.go) | [Account.Unlock and Derive](../../../core/crypto/crypto.go) |
| How does a file become durable ciphertext? | [Service.Import](../../../core/desktop/operations.go) | [PutContext](../../../core/sync/store.go), [EncryptStream](../../../core/crypto/stream.go) |
| What happens if sync loses a response? | [Client.Sync](../../../core/sync/client.go) | [push and request](../../../core/sync/client.go), [putRevision](../../../server/sync.go) |
| Why are there two versions of a document? | [Store.Heads](../../../core/sync/store.go) | [Store.Edit](../../../core/sync/edit.go), [appendChange](../../../server/sync.go) |
| Why does a scan become searchable? | [search.build](../../../core/search/index.go) | [Extractor.Text](../../../core/search/extract.go), [PDF extraction](../../../core/search/pdf.go) |
| What does the model actually run? | [nativeModel.EmbedTokens](../../../core/semantic/model_onnx.go) | [windowing](../../../core/semantic/semantic.go), [QueryMode](../../../core/search/semantic.go) |
| What protects restore success? | [runBackup](../../../cli/backup.go) | [Snapshot and Check](../../../core/sync/backup.go), [Publish](../../../core/local/files.go) |

Use `rg -n 'func .*PutContext' core/sync/store.go` or search for the listed symbol in your editor. Function names are more stable than line numbers as the working tree changes.

## Glossary

| Term | Meaning in this project |
| --- | --- |
| AEAD | Encryption that checks ciphertext authenticity and additional context |
| AAD | Visible context included in the authentication check |
| Argon2id | Memory-intensive password-to-key derivation |
| KEK | Password-derived key encrypting the master-key wrapper |
| Master key | Random collection key for metadata and wrapped file keys |
| File key | Random key for one content encryption |
| Nonce | Public per-encryption value that must not repeat with a key |
| Credential | Derived secret submitted to server login |
| Verifier | Domain-separated digest retained by the server |
| Session token | Random temporary bearer authorization from a login |
| Object | Stable logical document or folder |
| Revision | Immutable edition naming its base revision |
| Head | Revision with no child; multiple heads mean conflict copies |
| Pending | Durable intent to publish a local change |
| Cursor | Durable bookmark in server change ordering |
| Epoch | Identifier for a particular relay/deletion server-state generation |
| Sidecar | Encrypted internal document containing index/analysis data |
| Embedding | Model-generated vector representing text |
| Cosine similarity | Vector-angle similarity; a ranking signal, not probability |
| RRF | Fusion based on result positions across ranked lists |
| PRF | Repeatable secret-output function used by passkey convenience unlock |
| WAL | SQLite write-ahead log accompanying the database |
| Deletion fence | Durable record refusing resurrection of an erased object |

## Questions to test your mental model

1. Can a valid first chunk prove the whole bill arrived? No; completeness needs expected count, lengths, final marker, and whole-stream checks.
2. Can a server credential decrypt documents? Not directly; it authorizes server access and differs from the master key.
3. Can recovery reconstruct a deleted file? No; recovery unwraps a surviving key, not missing ciphertext.
4. Does sync success mean no conflicts? No; preserving conflict copies can be correct successful sync.
5. Is a synced word index private? At rest/in transfer its payload is encrypted; an unlocked client decrypts it to search.
6. Does a cosine score of 0.8 mean 80% accuracy? No; similarity and calibrated probability are different quantities.
7. Does building an ONNX binary prove the model works on your private PDFs? No; model assets, runtime acceptance, OCR quality, and collection-specific evaluation remain distinct.
8. Does permanent deletion erase an older backup? No; each independently stored copy has its own lifetime.
