# highlight.js, vendored

The code views' syntax highlighter (web/src/modules/code/infrastructure/hljs-highlighter.js),
committed as-is so building needs no package manager. Never edit these files; replace them.
Tracked here rather than in web/vendor/, which the repository's `vendor/` ignore rule hides.

- Package: `@highlightjs/cdn-assets` 11.11.1 (BSD-3-Clause, see LICENSE), the ES-module build.
- Source: https://registry.npmjs.org/@highlightjs/cdn-assets/-/cdn-assets-11.11.1.tgz
  (tarball sha512 `VEPdHzwelZ12hEX18BHduqxMZGolcUsrbeokHYxOUIm8X2+M7nx5QPtPeQgRxR9XjhdLv4/7DD5BWOlSrJ3k7Q==`,
  matching the registry's `integrity`).
- `core.js` is `package/es/core.js`; `languages/<id>.min.js` are `package/es/languages/<id>.min.js`.

Imported by bare name (`"highlight.js/core.js"`, `"highlight.js/languages/go.min.js"`), only from
`web/src/main.js` and `web/dev/main.js`: `cmd/webbuild` aliases `highlight.js` to this directory, and
`web/dev/*.html` map it with an import map.

## Upgrading

1. Download the tarball for the new version and check its sha512 against `integrity` in
   `https://registry.npmjs.org/@highlightjs/cdn-assets/<version>`.
2. Copy the files above over these, update this README and the table (`shasum -a 256`).
3. Run the web tests (the highlighter test runs every registered language).

| File | sha256 |
|---|---|
| `core.js` | `5ebc0de0077eb435df6aed653ffb9f0882853158dd0355e421a5d63feb92c7f6` |
| `LICENSE` | `6c081431591d9df696c82dc598fe1423765b8a299b200ed00b281afd0f64c490` |
| `languages/bash.min.js` | `d2290ecb0b7d522df0d91d54ee216c673e0c504decd48ef15197b3cab5cb7474` |
| `languages/c.min.js` | `e59b00b861c8aef367e69135fd38a0eb8199bbf176c8bdd47037f8c10f252f13` |
| `languages/cpp.min.js` | `36a947ebc97b59bee8c81ea1074dac5a2559da0bca2acac25f0587172bb29efd` |
| `languages/csharp.min.js` | `4a4af1a69c7d9122679f82f7f6fc6f4d0ab494f8dedc2874f8820d3bb0a23285` |
| `languages/css.min.js` | `8024c97edb80335f7aded04543de058770eab34939b9b2db8334fb8aa16f0e45` |
| `languages/diff.min.js` | `259e0a75cbaa5ce6769259ac647cec904a4c6b5cdc6e8d66f7820b01213a4492` |
| `languages/dockerfile.min.js` | `bd40da2c7d1b727dcd4e76f197f176e3d45ed61d167ef5d82a1f15e38a929de5` |
| `languages/go.min.js` | `364487e8fca398a6548efbeae11a6d4e28f35ae908c5324c0efd628e5359ddc5` |
| `languages/graphql.min.js` | `c228741470beb723a0dd54fdcc3f4a6326fa0772f8962faa9fadd834f4679325` |
| `languages/ini.min.js` | `a309557ec2ffe81630e3fbe94237d870b3db28275513303ea8d9e3a6863ad04e` |
| `languages/java.min.js` | `94cdadf61b344473f296d0873bf87bda1ea51fd09bc0b765bba4fd2b1e8f494c` |
| `languages/javascript.min.js` | `d893654be08f10fe656739ec83cf2cdeb7ea82a26b332b9fea88d4d215c00d89` |
| `languages/json.min.js` | `7dd0922802ac593d34da3466175377c9efb4612ee2ea97ddafa1fce618a510d9` |
| `languages/kotlin.min.js` | `25b777553b13af6c051ad53812c04d2e32110deb3d9445f19009dd7730589a19` |
| `languages/less.min.js` | `300ae4e2ab05493023abcfad15ce81624a9dcaafa35b541ac712dcf330a3a85f` |
| `languages/lua.min.js` | `0fe32dbe26f0b4579d23dfd49a449f1e91a592c560e67e68ea8a66c6994d03ad` |
| `languages/makefile.min.js` | `13f6006b2b59fe2f156cc2c38ebfe481878833502f9423748fd5c9ea4e7e0b93` |
| `languages/markdown.min.js` | `e8f544abc25490c51ff4bea5687a02ef4a65f1edc83dd208b6664d2672b3e3d5` |
| `languages/php.min.js` | `21b6762aa747e30b8f4c51effcba0511d902aa7f9cfef2e20265b29581333197` |
| `languages/plaintext.min.js` | `e7a15327bc6b71fedb3db4b79385860f628845c1553d755b04d1d91df9afc164` |
| `languages/protobuf.min.js` | `c720f54c191b9f9bb9b6963ee6317ad8d1a558aa1fdd37555546f95e38173c74` |
| `languages/python.min.js` | `4112a5c44dc7a4c56a772347bd318f246aa1ea51e0d6cb6aba78b25c3e60d524` |
| `languages/ruby.min.js` | `0bd34c5356e4a388836009eb3148cf142f698ded32e02d8f649c81efa1b9da41` |
| `languages/rust.min.js` | `9985e07ffd86b7172ce1499fadb1ae76677875b823fbc966009f324bffbb386e` |
| `languages/scss.min.js` | `509e373123ce0ffb2c2d8d5deb56c8e1a7731c1e1fcf1b220cea80d5440984cc` |
| `languages/sql.min.js` | `eadde8cadf1c469031abc0c6afd9767dfa8aacf9a7f9ab739ee6edfe635e300e` |
| `languages/swift.min.js` | `441342590da84988b81152e401b34d2221dc03384f47caff4ea2744904043a39` |
| `languages/typescript.min.js` | `60f542a05921072df100b34ea9a20a765617c518f8ac11d1c2c238ae11d83fcc` |
| `languages/xml.min.js` | `2d843265c0bdd3e50f107e6473196b5f525c0ea68de83c40f9f30ffbbea6896f` |
| `languages/yaml.min.js` | `3d227e0d347021b99da5c7b8de86ec8ce647f2f15d3b97100b34f9d2507ec2e7` |
