# Folder emoji artwork

The 32 unmodified 64px PNG files in `apple/` are Apple emoji artwork downloaded
from `iamcal/emoji-data`, revision
[`13ee711e222ea17fe537bfea953c687866f16411`](https://github.com/iamcal/emoji-data/tree/13ee711e222ea17fe537bfea953c687866f16411/img-apple-64).
The source README identifies this set as extracted from macOS 26.4 beta 4.

Apple images are copyright Apple Inc. The source README states that they are
not licensed for commercial usage. These images are **not** covered by Klefki's
AGPL license or by the emoji-data repository's MIT license for code and data.
Bundling them here does not grant additional rights to Apple artwork.

Source and image-rights notice:
https://github.com/iamcal/emoji-data/blob/13ee711e222ea17fe537bfea953c687866f16411/README.md#image-sources

Only this small folder-icon selection is bundled. Both clients import the same
files; the web build embeds their URLs and caches any emitted PNGs for offline
use. Choosing an icon makes no runtime request to an external artwork host.
