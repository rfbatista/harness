# Harness

## Checks

`make check` builds, vets and runs the tests with the race detector. It is what to run before calling work done.

### Page time

Every project page (the board, the task page, settings, documents, code, design, design system, usage and reviews) answers with its page server time and where it went, in a `Server-Timing` response header and in one `page time` line of the server log. The metrics are `total`, `db`, `git` (its description is the number of git calls), `render` and `frame` (the part of the page that is the shared project frame). Read them in the browser's devtools, Network tab, Timing.

`make page-bench` is the page benchmark. It requests every project page several times and prints each page's median and worst page server time with its breakdown. It only sends GET requests.

- `make page-bench` runs on the seeded four-times project (about 200 tasks, 20 live sessions, 3 repositories and a full agent catalog), in process, and touches none of your data.
- `make page-bench PAGEBENCH_URL=http://127.0.0.1:8080` runs against a live server, on its project `harness`; `PAGEBENCH_PROJECT=<name or id>` picks another one. A server that does not send `Server-Timing` yet is timed to its first byte and shown with "no breakdown".

`make perf-budget` is the page-time budget check. It runs the benchmark on the seeded four-times project and fails, naming the page, its median and its budget, when a page is over. The budgets are in `internal/app/testdata/page-budgets.json`: 300 ms per page and 100 ms for the frame share is the target, and a page that is not fixed yet carries a temporary budget above its measured time. Each fix task lowers the budgets of the pages it fixes. It is not part of `make check`, because its timings depend on the machine.
