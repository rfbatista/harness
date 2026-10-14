# htmx, vendored

Swaps the web UI's swap regions in place (ADR 0005, web/src/swap-regions.js),
committed as-is so building needs no package manager. Never edit these files; replace them.
Tracked here rather than in web/vendor/, which the repository's `vendor/` ignore rule hides.

- Package: `htmx.org` 2.0.11 (0BSD, see LICENSE), the ES-module build.
- Source: https://registry.npmjs.org/htmx.org/-/htmx.org-2.0.11.tgz
  (tarball sha256 `165a2655073e9e6e55e307a7c74e8ebcdb76afb7701f9c16cfd106a13add408e`,
  sha512 `Thx/WtpeOQqSrqBCw/A1cwGJGg4UrVa3+sW0GmrM3p4gJgO89ecH4qtbnyzDDWFvBTqjnIMCgELTNt636dtamA==`,
  matching the registry's `integrity`).
- `htmx.esm.js` is `package/dist/htmx.esm.js`; `LICENSE` is `package/LICENSE`.

Imported by bare name (`"htmx.org"`), only from `web/src/main.js`: `cmd/webbuild`
aliases `htmx.org` to `htmx.esm.js` here.

## Upgrading

1. Download the tarball for the new version and check its sha512 against `integrity` in
   `https://registry.npmjs.org/htmx.org/<version>`.
2. Copy the files above over these, update this README and the table (`shasum -a 256`).
3. Run the web adapter's page and browser tests (`go test ./internal/adapter/in/web/`).

| File | sha256 |
|---|---|
| `htmx.esm.js` | `b6554de9fb57e084ac992fc65ac5703f0d7f36b89e8a45f08905ca60cb665a1a` |
| `LICENSE` | `d3d2456f76414f2456104660ebd65aff1c04cd7966b942bdabd63f3cdb316a38` |
