# Inside the vault: Klefki technical guide

Open **[index.html](index.html)** directly in your browser. No server, hosting,
network access, npm install, or application startup is needed to read it. The
generated HTML includes all 18 rendered Markdown chapters, styles, scripts, and
16 SVG diagrams. You can also copy that HTML file alone to another machine.

The guide covers the source checkout reviewed on 16 October 2026:
application commit `bb78a53`, including the icon and sync-repair changes.
It focuses on core
technology and runtime behavior, including keys, records, databases, revisions,
sync, the server, local extraction/inference, organization, and recovery.
It excludes interface/styling explanations. Source descriptions do not establish
hosted deployment or physical-device acceptance; consult the linked implementation
records for that evidence. The video [recording script](../media/klefki-technical-video/script.md)
is a separate ten-minute plan, with rehearsal and recording still required.

Use chapter navigation, full-book search, section links, previous/next, theme,
text size, and printing of the selected chapter. Click or press Enter on a diagram to enlarge it. Read
markers and preferences are saved locally when the browser allows storage for
file origins; the reader still works when storage is unavailable. Left/right
arrow keys change chapters outside interactive controls.

The Markdown chapters remain the editable source in `chapters/`. Source links
open repository files beside this folder; a standalone copy of `index.html`
still renders everything, but those repository links need the checkout.
Official reference links open online only when clicked.

## Rebuilding after editing chapters

From the repository root:

```sh
python3 docs/technical-guide/build.py
```

The build uses Python 3 and [Python-Markdown](https://python-markdown.github.io/).
Python-Markdown was already available on the machine where this book was built;
it is needed only for rebuilding. If missing, install it in a virtual environment:

```sh
python3 -m venv /tmp/klefki-guide-build
/tmp/klefki-guide-build/bin/python -m pip install Markdown
/tmp/klefki-guide-build/bin/python docs/technical-guide/build.py
```

`diagrams.py` generates the SVGs with the Python standard library. `reader.html`,
`reader.css`, and `reader.js` define the reader. `build.py` renders Markdown tables,
code blocks, and heading anchors, validates local links, rewrites their paths,
and embeds diagrams and runtime assets into `index.html`. It does not fetch
anything or modify application code.

## Review verification

On 16 October 2026, rebuilding and local Markdown-link checks passed. Chromium
checks covered all 18 chapters and 16 embedded diagrams, navigation, section
anchors, search, saved read markers/preferences, keyboard diagram opening,
mobile navigation, selected-chapter printing, denied storage, no-JavaScript
reading, and a standalone copied HTML file. Reading through `file://` produced
no browser errors or HTTP requests. These checks validate the book, not the
application or a hosted deployment.

The video script has 11 contiguous edit slots totaling 10:00 and 1,270 spoken
words; every slot fits at 140 words per minute. Rehearsal, synthetic demo capture,
and recording remain separate work.

## Chapter list

1. [The system map](chapters/00-map.md)
2. [Startup and unlock](chapters/01-startup.md)
3. [Passwords and keys](chapters/02-keys.md)
4. [Records and chunks](chapters/03-records.md)
5. [Storage and revisions](chapters/04-storage.md)
6. [Mutations, trash, and deletion](chapters/05-mutations.md)
7. [The ciphertext server](chapters/06-server.md)
8. [Offline sync](chapters/07-sync.md)
9. [Relay and performance](chapters/08-relay.md)
10. [Session lifecycle and convenience access](chapters/09-lifecycle.md)
11. [Browser persistence and native boundaries](chapters/10-browser.md)
12. [Extraction and OCR](chapters/11-extraction.md)
13. [Keyword search](chapters/12-keyword-search.md)
14. [Local inference and hybrid search](chapters/13-semantic.md)
15. [Organization and corrections](chapters/14-organization.md)
16. [Backup and restore](chapters/15-backup.md)
17. [Security and verification](chapters/16-security-verification.md)
18. [End-to-end walkthrough and glossary](chapters/17-walkthrough.md)
