# How I built Klefki: an encrypted document vault that works offline

**Format:** approximately ten minutes, spoken walkthrough with screen recordings and diagrams.
**Audience:** developers comfortable with basic databases and HTTP; no cryptography or distributed-systems background assumed.
**Delivery:** explain this to one developer sitting next to you. Read the blockquotes aloud; everything else is a production cue. Leave small pauses when a diagram changes. Technical terms belong in the explanation, not in a rapid list of dependencies.

This script was reviewed against application commit `bb78a53` on 16 October 2026. Advisory device-pending reports and sync repair are explained in the technical guide; they are outside this video's ten-minute narrative. It is a recording plan, not evidence that its demonstrations have been recorded or passed. Use a separate synthetic demo account and invented files. Do not record your password, recovery code, bearer tokens, or personal documents.

The slots below total **10:00**. Aim for about 140 spoken words per minute and use the remaining time for the visual holds. A rehearsal determines the actual running time; the timestamps are edit targets, not measured narration. If a live operation takes longer, cut to its result rather than filling time with more feature descriptions.

## 0:00–0:45 · What I built

**Picture:** start on camera; cut to the document library after the first sentence. Show a few invented documents, then desktop and browser side by side. Hold the final shot briefly.

> I built a document vault called Klefki. You can keep your bills, IDs, certificates, and scans in it, organize them, and access the same collection from your desktop or browser.
>
> The part I wanted to get right was what happens underneath. Your documents are encrypted on your device, and you can keep working when you're offline.
>
> But that creates a few interesting problems. What happens if two devices change the same document? What if an upload succeeds, but the response never arrives? And how do you search a scan without sending its contents somewhere else?

**On screen:** `Local storage · Client-side encryption · Multi-device sync`.

## 0:45–1:35 · Show the product before the architecture

**Picture:** import a synthetic `poster.png`, rename it, move it into a folder, and show the browser receiving it. Then show an already indexed synthetic bill matching an identifier from its contents. Give the viewer a few seconds to read each result.

> Here's the basic flow. I add a document, give it a useful name, put it in a folder, and sync it to another device. I can also search inside supported documents, including scans, once they've been indexed.
>
> Desktop and the browser each keep their own encrypted local copy. So losing the connection doesn't stop me from opening documents or changing their details. Those changes wait locally until I sync again.
>
> The desktop app uses Wails, with Go underneath. There's also a TypeScript web app you can install as a PWA, and a CLI for things like backups and recovery.

**Demo accuracy:** the search shot must use a document actually indexed by desktop/CLI. Browser-only uploads do not automatically have an OCR index. Use a recorded completed sync for the receiving shot.

## 1:35–2:20 · The architecture

**Picture:** show [the system diagram](../../technical-guide/diagrams/system.svg). Highlight the clients, their local stores, and finally the server. Let the server boundary remain visible before the next section.

> The architecture is clients with local stores, connected through a Go server. Desktop uses SQLite. The browser uses IndexedDB.
>
> The clients encrypt and decrypt documents. The server handles login, stores encrypted chunks, and keeps a change log that devices can catch up with.
>
> Think of the server as a courier carrying sealed parcels. It can see parcel IDs, sizes, and delivery activity, but it doesn't have the document keys.
>
> This is where the distributed-systems part starts: several devices can change their own state independently, and then we have to bring those changes together over a connection that can fail.

**On screen:** `Offline-first clients → ciphertext replication server`.

## 2:20–3:30 · How the encryption works

**Picture:** show [the key hierarchy](../../technical-guide/diagrams/keys.svg), revealing one level at a time. Finish with three boxes representing a 9 MiB file's chunks; keep the diagram visible for a short pause.

> The password doesn't directly encrypt every file. I use Argon2id to derive password keys, and one of those keys opens a wrapper containing a random master key.
>
> That master key encrypts private metadata, like names and tags, and wraps separate keys for document content. Each new content version gets a fresh random file key.
>
> It's like using your password to open a keyring, then using the keys on that ring to open individual documents. The recovery code opens another wrapper around the same master key.
>
> Content is split into four-mebibyte chunks and encrypted with XChaCha20-Poly1305. Along with the bytes, authentication binds each chunk to its account, file, position, and whether it's the final chunk.
>
> So swapping chunks or cutting a file short gets rejected. The client still has to check the complete file; successfully decrypting one chunk doesn't prove that the rest arrived.

**On screen:** `Argon2id → KEK → master key → file keys` followed by `9 MiB = 4 MiB + 4 MiB + 1 MiB`.

**Delivery:** say “argon two I D,” “ex-cha-cha twenty, poly thirteen-oh-five,” and “four mebibytes.” Do not read binary offsets aloud.

## 3:30–4:20 · IDs, revisions, and durable local changes

**Picture:** show `poster.png` with three labelled IDs: `object O`, `revision A`, `content F`. Then highlight [Store.Edit](../../../core/sync/edit.go) reusing `r.FileKey`, and [the local store](../../../core/sync/store.go). Pause on `COMMIT`.

> A document has a stable object ID. Its filename is just encrypted metadata. Each change creates a revision with its own ID and a reference to the revision it started from.
>
> If I rename poster dot png, I create new encrypted metadata, but reuse the existing image chunks. There's no reason to upload the whole image again.
>
> When I import content, the store saves the encrypted records and marks the revision pending in one database transaction. Either that local change commits together, or it rolls back.
>
> That pending flag is the durable queue. A network failure can leave work waiting, but the document is already saved locally.

**On screen:** `Encrypted chunks + revision + pending flag → one local transaction`.

## 4:20–5:40 · Sync and the failures between steps

**Picture:** show [the sync diagram](../../technical-guide/diagrams/sync.svg). Reveal upload, publication, receiver commit, then acknowledgement. Briefly cross out a response arrow and animate a retry of the same revision ID. Pause on the acknowledgement boundary.

> During sync, the client uploads pending revisions in order, so a parent gets published before its child. It checks which chunks the server already has, uploads the missing ones, and then publishes the revision.
>
> That last step matters. Uploading some chunks isn't the same as publishing a complete document revision.
>
> Now suppose publication succeeds, but the response gets lost. The client retries the same revision ID and bytes. The server recognizes that exact retry rather than creating another revision. That's idempotency.
>
> The receiving device reads the change log from its local cursor, which is basically a bookmark. It authenticates the metadata, key, and all the expected chunks, then saves the revision and updated cursor in one transaction.
>
> Only after that does it acknowledge receipt to the server. If the download fails, the cursor stays behind that revision. If the acknowledgement fails, the saved data is still there, and the receipt can be sent again.
>
> So the guarantee isn't “the network delivers exactly once.” It's that retries are safe and progress follows durable storage.

**On screen:**

```text
Upload chunks → publish revision → clear pending
Authenticate → commit revision + cursor → acknowledge
```

## 5:40–6:55 · Concurrent edits and conflict copies

**Picture:** start with [the revision graph](../../technical-guide/diagrams/revisions.svg). Use a simpler first state: A forks into B and C. Then show the two resulting names in a synthetic conflict demonstration or clearly labelled explanatory graphic. Hold the result.

> Here's a conflict that can happen. Desktop and my phone have the same poster at revision A. Before either device receives the other's change, I rename it to poster one on my phone and poster two on desktop.
>
> Both edits say they started from A. Now the history has two branches. Neither has seen the other, so I preserve both as conflict copies. Upload order doesn't select a winner.
>
> This is optimistic concurrency with a revision graph. A head is a revision with no child, and two heads for the same object tell us there are competing versions.
>
> Since these are renames, both versions reuse the image chunks. The extra storage is metadata and history.
>
> Importing two files with the same name is different: they get different object IDs. And desktop doesn't have an image-content editor. Content replacement is supported through the CLI, and the browser can edit small text files. So this example is a metadata conflict, not two people editing the picture.

**On screen:**

```text
         A: poster.png
        /             \
B: poster-one.png   C: poster-two.png
        same object, same image chunks
```

**Demo accuracy:** start both clients from the same synced revision and disconnect them before renaming. Two unrelated imports are duplicates, not a conflict demo. No automatic content merge or last-write-wins claim.

## 6:55–7:50 · Searching private documents

**Picture:** show a synthetic scanned bill, then [the extraction diagram](../../technical-guide/diagrams/extraction.svg) and [the index diagram](../../technical-guide/diagrams/search.svg). Show a real match for an invented meter ID. Leave the evidence readable.

> Search has a similar ownership rule: the client does the private work. Desktop and CLI extract selectable PDF text using Poppler, and use Tesseract for images and scanned content. Mixed PDFs can need both.
>
> Then I build a word-frequency index from the text, filenames, and tags. Queries use BM25-style ranking, so a rare meter number is more useful than a word that appears in every bill.
>
> That index is itself an encrypted internal document. It syncs using the same storage and revision machinery.
>
> So the browser can search an index built on desktop without repeating OCR. But it doesn't do browser OCR itself, and its new uploads need desktop or CLI indexing for content search.

**On screen:** `Local extraction → encrypted index → local query`.

## 7:50–8:40 · Optional local semantic search

**Picture:** show [the semantic pipeline](../../technical-guide/diagrams/semantic.svg). Display the model name and one descriptive query beside a keyword query. If model assets/indexes aren't ready, use the diagram rather than presenting an invented result. Pause between the two rankings.

> For queries where the wording doesn't match, there's optional semantic search. I run BGE-small locally with ONNX Runtime, turning text into three-hundred-and-eighty-four-dimensional vectors.
>
> Related text should point in similar directions in that vector space. The document vectors are encrypted and synced; the querying client embeds its own query locally.
>
> Hybrid search combines keyword and semantic result positions using reciprocal rank fusion. It avoids directly adding scores from two different scales.
>
> A similarity score isn't a confidence percentage. I filter weak matches, and category suggestions remain reviewable. This is English-first, opt-in, and its quality still needs checking against real document collections.

**On screen:** `BM25 ranks + cosine-similarity ranks → reciprocal rank fusion`.

## 8:40–9:20 · Where the sync latency came from

**Picture:** show ten request arrows becoming three, labelled `Historical 84-revision no-edit benchmark`. Then reveal four concurrent chunk arrows. Optionally show the historical timing pair from [the benchmark report](../../benchmarks/2026-10-13-relay-optimization/README.md), with its date/methodology. Let the arrows settle.

> One performance problem was round trips. Even when there were no edits, the client could spend seconds making several small requests to the hosted server.
>
> I batched relay preparation and history checks. In the recorded eighty-four-revision no-edit case, that reduced ten production requests to three.
>
> For larger files, I allow up to four chunk transfers together, but keep revision publication ordered. I also narrowed server locking so slow network transfers don't hold a global relay lock.
>
> Those optimize different costs: request latency and moving the actual payload.

**Accuracy:** the request count refers to that benchmark's protocol revision. Later deletion synchronization and device-state reporting add protocol work. Historical timings are measurements, not a live demonstration or universal speed guarantee.

## 9:20–10:00 · The limits and what this taught me

**Picture:** return to camera. Briefly overlay `Replication ≠ backup`, then finish on desktop/browser with a small project-name card. Leave a short closing hold; avoid a second feature montage.

> There are tradeoffs. Keeping revision history costs storage. Sync doesn't replace a backup, so the CLI creates consistent snapshots and authenticates their contents during restore.
>
> Encryption doesn't hide sizes or timing, and it doesn't protect an already compromised device or browser origin. Complete server rollback is also outside the current guarantees.
>
> For me, the interesting part of building Klefki was deciding what “saved” means. Saved locally, published remotely, and acknowledged by another device are three separate events. Keeping those boundaries clear is what makes the failure cases manageable.

## Recording notes

Use the script as talking points during a rehearsal, then keep the phrasing you naturally use. Don't add “seamless,” “military-grade,” “production-ready,” or confidence percentages. No personal build timeline or specific motivation is invented here; add a true one-sentence origin story if you want it.

The product section is deliberately brief. Most of the runtime is about architecture, encryption, local durability, distributed replication, and search. The script doesn't spend time on React components, styling, or a dependency tour.

For the distributed explanation, “eventual replication” means clients can learn the same set of revisions. It does not mean conflicts automatically disappear. “Idempotent publication” is safer than “exactly-once delivery.” Stable relay device IDs and login session IDs are distinct; the video doesn't need both on screen.

For recording, prepare a synthetic `poster.png`, a text file, and a scanned bill with an invented identifier. Establish the same base revision before a conflict scene. Confirm a content-search match after real indexing. A diagram can explain a failure without pretending a successful live failure-injection test was recorded. Label explanatory graphics accordingly.

Temporary relay is an optional extension, not a default guarantee that the server retains no data. If you add a sentence about it, replace existing narration to keep the runtime: “There's also a temporary relay mode that clears delivered server ciphertext after all linked devices have acknowledged saving it. Local copies and backups then matter even more.” At least two linked devices are required.

## Source references for technical review

| Claim | Source |
| --- | --- |
| Password keys, master wrappers, file keys, AEAD | [Go crypto](../../../core/crypto/crypto.go), [format](../../crypto-spec.md) |
| Chunk count and complete-file validation | [Streams](../../../core/crypto/stream.go) |
| Atomic local imports and authenticated received revisions | [Store](../../../core/sync/store.go) |
| Metadata edits reuse wrapped file keys | [Store.Edit](../../../core/sync/edit.go) |
| Ordered push/pull, retry, pending flags, acknowledgement | [Client](../../../core/sync/client.go) |
| Idempotent revision publication and sequence log | [Server sync](../../../server/sync.go) |
| Browser storage and small text editor | [Store](../../../web/src/store.ts), [editDialog](../../../web/src/main.ts) |
| CLI content replacement | [CLI add --object/--base](../../../cli/sync.go) |
| Local PDF/image extraction and encrypted BM25 index | [Extraction](../../../core/search/extract.go), [PDFs](../../../core/search/pdf.go), [index](../../../core/search/index.go) |
| Local model, vectors, hybrid fusion | [Manifest](../../../core/semantic/manifest.json), [ONNX](../../../core/semantic/model_onnx.go), [fusion](../../../core/search/semantic.go) |
| Batching and bounded chunk concurrency | [Benchmark evidence](../../benchmarks/2026-10-13-relay-optimization/README.md), [chunk windows](../../../core/sync/chunks.go), [server locks](../../../server/relay_lock.go) |
| Snapshot and authenticated restore | [Backup core](../../../core/sync/backup.go), [CLI restore](../../../cli/backup.go) |
| Security limitations | [Threat model](../../threat-model.md) |

For deeper explanations, open [the offline technical book](../../technical-guide/index.html).
