# Organize files with local embeddings

The desktop **Organize files** action proposes tags for the whole active library.
It reuses the installed BGE model and encrypted document indexes. Enable
**Semantic search and suggestions** in Settings first, using an ONNX-enabled
desktop build. Documents are processed locally; a missing or stale index is
rebuilt before suggestions are returned.

1. Click **Organize files**, beside the library sort control.
2. Optionally enter custom tags, separated by commas, such as `Recipes, Travel`.
   The six built-in categories and existing document tags are included automatically.
3. Click **Find tags**. Review each proposed group and its files.
4. Rename a proposed tag or choose another existing tag in its input. Uncheck
   files to exclude them, then click **Apply tags**.

Saving adds one reviewed tag per selected file. Existing tags, names, folders,
favorites, file contents, and revision history are retained. New tags appear in
the library's tag filters. Tags are encrypted as normal metadata revisions,
persist offline, and use the existing sync queue and backup format. If any
selected revision is stale or invalid, the entire batch is rejected.

## Matching and new tags

The organizer compares indexed document sections with category descriptions.
When a section has a confident category match, it uses section similarities
instead of averaging away that evidence. Confident sections that disagree cause
it to abstain from built-in categories. If no section has a confident match, it
checks the averaged document embedding. Custom tags use averaged document
embeddings, tag names, and centroids from documents already carrying each tag.
Built-in categories keep their defined meaning; existing tags do not redefine
them. For example, a resume tagged Identity does not make a similar job
description an Identity document. Work also matches a description of job
postings, responsibilities, qualifications, skills, salary, and benefits.
A document is excluded from its own custom tag centroid. The configured
classification cutoff and winning margin control whether an existing tag fits;
similarities are ranking scores, not probabilities. A single confirmed category takes
precedence over model matching.

Untagged documents with no confident match are grouped by content. Each member
must have cosine similarity at least 0.72 to both the group seed and its current
centroid. Groups get editable names from up to two distinctive indexed keywords,
with filename weighting and common-word filtering. These names are keyword
suggestions; the embedding model does not generate prose. A lone unmatched file
can also receive a new proposed tag. Proposed tags only become examples after
you apply them.

Files already carrying tags are skipped when no confident additional match
exists. Files with unresolved revision or analysis conflicts, no indexed content
vector, or 64 tags are skipped. Trash, descendants of trashed folders, folders,
and internal index/correction files are excluded. Existing tag candidates are
bounded to 256, and a preview accepts at most 64 additional custom tags.

## Verification

- Go regressions cover existing-tag examples, custom-tag matching, distinct
  unmatched groups, editable valid names, confirmed-category precedence,
  cancellation, deterministic previews, and stale indexes.
- Batch tests cover atomic rollback, stale/duplicate inputs, invalid/reserved
  tag names, retained content and metadata, and case-insensitive duplicate tags.
- A two-client TLS test verifies that reviewed tags survive an offline restart,
  sync with their original content binding and history, and leave no known fixture
  tag/name/content plaintext in local or server storage.
- Desktop service tests cover lock/session generation checks and trash exclusion.
- A real BGE ONNX test matches an invoice to Money and two recipes to a custom
  Recipes tag. With a stricter cutoff, the recipes form one new group and the
  invoice forms another. Applying tags preserves all three plaintext contents.
- A BGE ONNX regression checks that a job description matches Work when a
  similar resume is already tagged Identity, including hospital and law-firm
  vacancies. Section regressions cover company background overwhelming the
  document average, conflicting categories, and aggregate evidence.
- All 26 desktop frontend tests and its production build pass. Three Chromium
  cases using an explicit fixture bridge cover review, group renaming, exclusions,
  tag filtering at 390/1200 px, and clearing the review on lock. These browser
  cases do not exercise native Wails bindings or real inference.
- The full Go race suite, focused static checks, and ONNX-enabled native desktop
  compilation/tests pass.

This is a desktop organization workflow. Saved tags remain compatible with web
and CLI clients; those clients do not yet expose this bulk review screen. Native
window acceptance and quality on a private document collection remain unverified.
M8's existing private-document acceptance requirement is still pending.
