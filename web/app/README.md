# web/app: the React web client

The React and TypeScript part of the web client (ADR 0100, ADR 0101). Its first
part is the top bar, the rail (the Barra lateral: project navigation and the Tasks list) and the
header's Tasks button, server-rendered islands in the templ pages (ADR 0001 and 0002 of the rail
migration: behaviour parity, driven by the project feed, not by the Alpine store). The Alpine
client in `web/src` serves everything else for now.

    src/features/<feature>/   navigation, notifications, rail
      domain/                 rules and values; imports nothing outside itself
      application/            use cases and the ports they need; imports only the domain
      infrastructure/         adapters (HTTP, browser events, shared stream) and in-memory adapters
      presentation/           presenters (framework-free, MVP) and passive views
      testing/                fixtures and the contract suite of each port
    src/shared/               kernel (presenter base, feed types), infrastructure (API client, shared stream), presentation helpers
    src/composition/          the composition roots: server (props only) and browser (hydrates every [data-island])
    src/islands/              the server entries gohydrate renders
    src/main.client.tsx       the client entry

    npm ci                    install (nothing here bundles: gohydrate does)
    npm run typecheck         tsc --noEmit, strict
    npm run lint              ESLint with the import-boundary rules of ADR 0101 (eslint-rules/layers.js)
    npm test                  node --test, TypeScript transpiled by esbuild

Tests sit at two seams: presenters through the use cases on the in-memory
adapters, and one contract suite per port against its HTTP (or browser) adapter
and its in-memory adapter. Views and the server render are covered by the
server's end-to-end tests.
