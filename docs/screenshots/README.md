# Screenshot sources

The README images show the current Klefki desktop frontend rendered in Chromium
with the generated sample library from
[the browser fixture](../../desktop/frontend/e2e/library.spec.ts).
They contain no private documents, passwords, or recovery codes.

- `library-dark.png`: document library in dark mode.
- `library-light.png`: the same library after changing Appearance to light.
- `locked.png`: the password screen after locking the library.

These are screenshots of the app's frontend with a fixture bridge. They do not
demonstrate native Wails dialogs, a physical fingerprint scan, or live sync.

To refresh them, build the frontend and run the existing screenshot case:

```sh
npm --prefix desktop/frontend run build
cd desktop/frontend
PW_DISABLE_TS_ESM=1 node node_modules/@playwright/test/cli.js test --grep 'reference layout'
```

Copy the three images from `desktop/frontend/test-results/` into this directory.
