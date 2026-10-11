# Klefki planned features

Status: planned, not scheduled. This records future work agreed on 2026-10-11;
it does not mark features as implemented or replace the current milestone
acceptance requirements. Check items only after their acceptance criteria pass.

## General vault organization

- [ ] Build an organizer for varied documents, unfamiliar file types, custom
  tags, and folders. Evaluate general behavior rather than patching individual
  misclassified examples.

### Proposed approach

1. Extract text and useful structure, including titles, headings, abstracts,
   and OCR where supported. Detect poor or unsupported extraction and show a
   review state instead of making an unsupported classification.
2. Assess document type, purpose, and topics separately, with supporting text
   evidence. Author names and incidental subject matter should not determine
   the main category.
3. Match the assessment to built-in categories and existing custom tags. Allow
   multiple useful tags and editable new-tag suggestions when existing tags do
   not fit. Consolidate equivalent proposed tag names.
4. Surface weak or conflicting evidence, alternatives, and a short reason for
   each suggestion. Explicit user corrections take precedence; unconfirmed
   predictions must not become trusted training examples.
5. Review changes before applying them as encrypted metadata revisions.

Benchmark a small local instruction model against a trained classifier using
the existing embeddings. Keep BGE for semantic search, related-file matching,
and tag similarity. Choose the classification model only after measuring
quality, memory use, and latency on representative documents. Any classification
fine-tuning must be evaluated separately from the search model.

Documents, extracted text, and model processing stay on the device. Model,
dependency, and packaging choices remain to be decided.

### Folder organization

- [ ] Include eligible folders in organization previews and reviewed tag batches.
- [ ] Suggest folder tags from the folder name and the classifications of its
  contents, including nested folders without counting documents repeatedly.
- [ ] Support shared tags for consistent contents and several tags or a project
  tag for mixed contents. Empty folders rely on their name and user review.
- [ ] Keep folder tagging and child tagging separately reviewable. Applying a
  folder tag must not silently tag or move its children.
- [ ] Recompute suggestions when contents change while preserving saved user tags.

### Acceptance

- [ ] Establish a labelled evaluation collection covering varied real PDFs,
  text files, scans, long documents, unfamiliar types, ambiguous documents,
  existing custom tags, and empty, nested, and mixed folders.
- [ ] Separate training, threshold selection, and held-out testing; keep related
  document templates out of different splits. Do not commit private documents.
- [ ] Compare the current organizer, a trained classifier, and a local instruction
  model on the same collection. Report wrong tags, missed useful tags, review
  rate, latency, and memory use; choose acceptance targets before final testing.
- [ ] Verify user corrections, new-tag consolidation, multiple tags, and honest
  review states for unsupported or uncertain inputs.
- [ ] Verify atomic application, stale-preview rejection, lock/session behavior,
  retained contents and existing tags, offline persistence, and encrypted sync
  for both files and folders.
- [ ] Complete a native-app walkthrough on the user's collection. Synthetic
  regressions alone do not establish general organization quality.

## Android app

- [ ] Build a dedicated Android app. Select the app framework and shared-core
  integration before implementation; the existing PWA remains available.
- [ ] Support account creation/import, password and recovery unlock, an encrypted
  offline library, file import/export, folders, tags, previews, and locking.
- [ ] Integrate the existing sync protocol, durable offline edits, retry behavior,
  and visible conflict copies.
- [ ] Evaluate camera/document capture, system file sharing, biometric unlock,
  and local OCR/search/organization against mobile resource and lifecycle limits.
  Show capabilities explicitly when a device cannot run a feature.

Acceptance: shared crypto vectors and cross-client document/metadata round trips
pass; offline changes survive restart; interrupted sync preserves data; locking
clears private views and active keys. Verify on a physical Android device and
document supported versions, packaging, recovery, and remaining feature gaps.

## iOS app

- [ ] Build a dedicated iPhone/iPad app. Select the app framework and shared-core
  integration before implementation.
- [ ] Support account creation/import, password and recovery unlock, an encrypted
  offline library, file import/export, folders, tags, previews, and locking.
- [ ] Integrate the existing sync protocol with durable offline edits, visible
  conflicts, and clearly documented background-execution behavior.
- [ ] Evaluate Files/share-sheet integration, document capture, biometric unlock,
  and local OCR/search/organization within device resource limits.

Acceptance: shared crypto vectors and Android/desktop/web interoperability pass;
offline changes survive restart; interrupted sync and app lifecycle transitions
preserve data; locking clears private views and active keys. Verify on physical
Apple devices and document supported versions, signing/distribution, recovery,
and remaining feature gaps.

## macOS desktop support

- [ ] Extend the desktop experience to macOS and decide supported OS versions
  and CPU architectures.
- [ ] Verify the desktop runtime, local store, file dialogs, previews, import/export,
  sync, recovery, locking, and platform lifecycle behavior.
- [ ] Package and verify the required local OCR and inference runtimes for each
  supported architecture; clearly expose unavailable capabilities.
- [ ] Evaluate platform credential storage and biometric unlock separately from
  the existing Linux fingerprint implementation.
- [ ] Provide reproducible builds, installation instructions, and a decision on
  signing/notarization and distribution.

Acceptance: shared vectors, cross-client sync, offline restart, recovery, and
app locking pass on each claimed architecture. Verify a native macOS walkthrough;
a successful cross-build alone does not establish platform support.

## Other existing future work

Expiry reminders, shared libraries/link sharing, and emergency access remain
unscheduled. Broader local LLM features beyond organization also remain future
work. Scope these separately before implementation.

Current implementation and evidence: [milestones](MILESTONES.md),
[project plan](PLAN.md), and [documentation](docs/README.md).
