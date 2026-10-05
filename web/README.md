# web/ — the browser half of the web client

Plain JavaScript ES modules running on Alpine.js (CSP build). Every Alpine
component is registered with `Alpine.data()`. The architecture (modules,
layers, the SSR contract with the templ BFF) is in [`docs/WEB.md`](../docs/WEB.md);
the CSS is [`design-system/`](../design-system/README.md).

```
src/
  main.js                       composition root (real infrastructure)
  shared/                       kernel: domain (errors, feed status), infrastructure
                                (api, feed, storage, clock), presentation (seed,
                                errors, format, shell components), testing
  modules/<module>/
    domain/                     rules + ports.js (the contracts infrastructure fulfils)
    infrastructure/             gateways over /api and SSE, DTO mappers, memory gateway
    presentation/               pages/, components/, view.js, register.js
    testing/                    fixtures, stub API, the port's contract suite
dev/                            pages that run the real modules on the memory gateway
test/                           browser test page; all.js lists every *.test.js
testdata/views/                 fixtures shared with the Go BFF's tests
vendor/                         Alpine, pinned (see vendor/README.md)
```

## Running it today

Until `cmd/webbuild` and the BFF exist, everything runs as native ES modules
from any static server rooted at the repository:

```bash
python3 -m http.server 8413        # from the repo root
open http://127.0.0.1:8413/web/test/index.html     # unit tests (46 passing)
open http://127.0.0.1:8413/web/dev/sessions.html   # the Sessions page, in memory
```

Disable the browser cache (or use a no-cache server) while editing: modules are
cached aggressively. On the dev page, `window.harness` simulates the server:
`harness.finish("s-port")`, `harness.ask("s-suite")`, `harness.drop()`,
`harness.reconnect()`.

## Adding code

- A new test file goes into `test/all.js` (the build will generate this list later).
- Follow the layer rules in `docs/WEB.md`: domain imports nothing outward,
  presentation never imports infrastructure, modules never import each other,
  only `main.js` and `dev/main.js` construct infrastructure.
- Markup binds properties and calls methods; logic lives in the component.
