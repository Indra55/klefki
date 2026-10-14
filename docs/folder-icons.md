# Per-folder emoji icons

Requested 2026-10-14: choose a separate emoji for each folder on desktop,
web and PWA, using Apple-style artwork consistently on all operating systems.

## Metadata extension (approved 2026-10-14)

Add optional `folder_icon` to the existing encrypted metadata JSON for folders.
Its value is a lowercase Unicode image identifier, for example `1f393` for the
graduation cap. It is an identifier, never a URL or filesystem path. Accept up
to eight codepoints, each four to six lowercase hex digits separated by hyphens,
and reject zero or invalid Unicode scalar values. Files cannot carry this field.
Missing or empty means the default folder icon; writers omit empty values.

This uses the existing authenticated metadata encryption and immutable revision
sync. It changes no keys, envelopes, database schema or server API. Icons travel
with their folder through rename, moves, favorites, trash, restore and backups.
An omitted icon in an edit preserves the current choice; an explicit empty
string resets it. Existing folders need no migration. An unknown valid icon
identifier displays the default artwork without changing stored metadata.

Older clients ignore unknown metadata fields. Older Go clients may drop this
field when rewriting a folder, so update linked clients before editing icons.

The user approved this optional metadata extension on 2026-10-14, as required
by `PLAN.md` before implementation in the codec.

## UI and artwork

- A compact, labelled picker offers 32 emoji choices in New folder and folder
  details, including a default-folder reset.
- The chosen artwork appears beside folder names and in folder breadcrumbs.
- Both clients bundle the same curated assets, including offline PWA
  caching. Do not request artwork from a third-party host at runtime.
- Use Apple images from `iamcal/emoji-data`, pinned to a source revision. Keep
  the artwork notice separate from Klefki's AGPL license. The source README
  states Apple images are copyright Apple Inc. and are not licensed for
  commercial usage; its MIT data/code license does not license Apple artwork.
  Source: https://github.com/iamcal/emoji-data#image-sources

## Acceptance

- Go and TypeScript agree on valid/invalid icon metadata.
- A selected icon survives encrypted sync, edits, trash/restore and restart.
- Existing folders and unknown icon identifiers render a default folder icon.
- Keyboard and mobile users can select, change and reset icons without
  overflowing the viewport; choices retain accessible names.
- Build both clients, verify offline browser rendering, rebuild/install Linux,
  then commit and push issue 2 before moving to issue 3.
