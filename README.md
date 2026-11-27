# Harness

## Build dependencies

Go, plus Node and npm for the React web client in `web/app`. `make web` runs `npm ci` there and bundles the React islands through gohydrate (a `replace` in `go.mod` points to the local checkout); Node runs only at build time, never when the server serves a request.

## Build tags

The server's conversation search uses SQLite FTS5, which the driver compiles in only with the `sqlite_fts5` build tag. The Makefile and `.air.toml` pass it. For `go build`, `go run` and `go test` typed by hand, run `go env -w GOFLAGS=-tags=sqlite_fts5` once. A server built without the tag still runs: search then answers `SEARCH_UNAVAILABLE`.

## Pairing the person

The routes that are yours alone (answering branch, push and service requests, the agent system settings, instruction-proposal decisions, the gate-run trigger) accept only a person credential (ADR 0016). With nobody paired, the server prints a one-time pairing link in its own terminal output when it starts:

```
Pair this browser with the harness, once, so it can answer requests:
  http://127.0.0.1:8080/api/person/pair?code=…
```

Open it in the browser: it sets an HttpOnly cookie and goes to the UI. The link works once and only until the server restarts or a day passes; the credential outlives restarts. For the terminal client and the bench command, pair once with the code from the link: `tui-client pair --code CODE`. The credential goes into one file, `~/.coding-pool/person.cred` (0600, in a 0700 folder), which every session's sandbox profile denies; it is never an argument or an environment variable, which any process of yours can read. There is no keychain path: a session could ask the `security` tool for it by its full path. A paired browser can ask for another code from `POST /api/person/pairing-codes`. A refused call answers `PERSON_ONLY`. A session run with the agent sandbox off runs as you and is not stopped by this.

## Checks

`make check` builds, vets and runs the tests with the race detector, then runs the React client's type check, lint and tests (`npm run typecheck`, `npm run lint`, `npm test` in `web/app`). It is what to run before calling work done.

### Page time

Every project page (the board, the task page, settings, documents, code, design, design system, usage and reviews) answers with its page server time and where it went, in a `Server-Timing` response header and in one `page time` line of the server log. The metrics are `total`, `db`, `git` (its description is the number of git calls), `render`, `frame` (the part of the page that is the shared project frame) and `island` (the server render of the React top bar, which is part of the frame). Read them in the browser's devtools, Network tab, Timing.

`make page-bench` is the page benchmark. It requests every project page several times and prints each page's median and worst page server time with its breakdown. It only sends GET requests.

- `make page-bench` runs on the seeded four-times project (about 200 tasks, 20 live sessions, 3 repositories and a full agent catalog), in process, and touches none of your data.
- `make page-bench PAGEBENCH_URL=http://127.0.0.1:8080` runs against a live server, on its project `harness`; `PAGEBENCH_PROJECT=<name or id>` picks another one. A server that does not send `Server-Timing` yet is timed to its first byte and shown with "no breakdown".

`make perf-budget` is the page-time budget check. It runs the benchmark on the seeded four-times project and fails, naming the page, its median and its budget, when a page is over. The budgets are in `internal/app/testdata/page-budgets.json`: 300 ms per page and 100 ms for the frame share is the target, and a page that is not fixed yet carries a temporary budget above its measured time. Each fix task lowers the budgets of the pages it fixes. It is not part of `make check`, because its timings depend on the machine.
