---
name: crap-metric
description: Score code by Change Risk Anti-Patterns (CRAP) — cyclomatic complexity combined with test coverage — using the `crap` CLI already installed on this system. Use when prioritizing what to refactor or test next, reviewing a complex change, or setting a CI risk gate. Covers Go, JavaScript, TypeScript/TSX, and Python.
---

# CRAP Metric

CRAP (Change Risk Anti-Patterns) scores each function by combining **cyclomatic
complexity** with **test coverage**, because neither alone tells you what's
actually risky: complex-but-well-tested code is fine, and simple-but-untested
code is usually fine too — it's the *combination* of high complexity and low
coverage that's expensive to change safely.

```
CRAP(m) = complexity(m)^2 * (1 - coverage(m))^3 + complexity(m)
```

The tool for this is the `crap` CLI, already installed on this system (`crap
-h` for its own full reference) — use it directly rather than eyeballing
complexity or coverage separately.

## When to Use

- Deciding what to refactor or write tests for next, instead of guessing
- Reviewing a large/complex diff and wanting an objective risk signal
- Setting up a CI gate against regressions in risky code
- Triggers: CRAP score, change risk, refactor priority, what to test next,
  complexity + coverage

## Basic Usage

```bash
crap                          # scan "." with no coverage — complexity-only, weak signal
crap ./internal                # scan a specific path (file or directory, recursive)
crap -cover=cover.out ./internal          # Go: score against a real coverage profile
crap -lcov=coverage/lcov.info ./src       # JS/TS/Python: score against an LCOV file
```

**Always pair a coverage source with the scan.** Without `-cover`/`-lcov`,
every function is assumed to be at 0% coverage, so the score degrades to
"just complexity" — a much weaker signal than complexity + real coverage.

## Generating Coverage First

```bash
# Go
go test -coverprofile=cover.out ./...
crap -cover=cover.out ./internal

# JS/TS/Python — anything that emits LCOV (nyc, jest --coverage, coverage.py + lcov exporter)
crap -lcov=coverage/lcov.info ./src
```

## Useful Flags

| Flag | Purpose |
|---|---|
| `-cover=FILE` | Go coverage profile (`go test -coverprofile=FILE`) |
| `-lcov=FILE` | LCOV coverage file (JS/TS/Python) |
| `-top=N` | Only show the N riskiest functions, instead of the full table |
| `-min-crap=N` | Only report functions scoring at or above N |
| `-exclude=PATTERN` | Glob/substring exclusion; repeatable — use for generated code, vendored deps, one-off scripts |
| `-include-tests` | Also analyze test files (skipped by default) |
| `-json` | JSON output instead of a table — for dashboards, PR comments, trend tracking |
| `-fail-over=N` | Exit 1 if any function exceeds N — CI regression gate |

Directories matching common build/vendor conventions (`.git`, `vendor`,
`node_modules`, `dist`, `build`, `bin`, `.next`, `__pycache__`, `.venv`,
`venv`) are always skipped automatically.

## Output

Table (default) — score, complexity, coverage (with a `*` marking "no
coverage data found, assumed 0%"), function name, and `file:line`:

```
CRAP   COMPLEXITY  COVERAGE  FUNCTION            LOCATION
930.0  30          0.0%*     dispatch            agents/agent_launch.py:609
110.0  10          0.0%*     agent_summary_line  agents/agent_launch.py:519
```

JSON (`-json`) — one object per function:

```json
{
  "file": "agents/agent_launch.py",
  "function": "dispatch",
  "start_line": 609,
  "end_line": 704,
  "complexity": 30,
  "coverage": 0,
  "coverage_known": false,
  "crap": 930
}
```

`coverage_known: false` is the JSON equivalent of the table's `*` — no
coverage data was supplied for that function, so `coverage` is the assumed
worst case, not a measured value. Don't treat a `coverage_known: false`
result as "this function has 0% coverage" — treat it as "coverage wasn't
measured here," and get a real coverage source before trusting the score.

## Workflow

1. **Generate real coverage first** (`go test -coverprofile=...` or an LCOV
   export) — a complexity-only run is a starting point, not a verdict.
2. **Run `crap` with `-top` or `-min-crap`** to focus on the highest-risk
   functions rather than reading the whole table.
3. **Interpret the score, not just complexity:**
   - High CRAP + low/no coverage → write tests for this function before
     touching it, or before trusting a change to it.
   - High CRAP + solid coverage → the tests reduce risk, but the complexity
     itself is still worth simplifying if it's touched often.
   - Low CRAP → not where refactor/test effort pays off right now.
4. **Exclude noise** (`-exclude`) — generated code, vendored packages,
   scripts — rather than trying to write one pattern that matches everything.
5. **Wire `-fail-over` into CI once a baseline exists**, not at the start —
   it's a regression gate, not a retroactive judgment on the whole codebase.
   The tool's own docs cite ~30 as the commonly used "needs attention" line,
   but calibrate against this codebase's actual distribution rather than
   applying that number blindly on day one.

## Notes

- Supports Go, JavaScript, TypeScript (incl. TSX), and Python in one run —
  no need to run it per-language separately if the target path spans more
  than one.
- Test files are skipped by default (`-include-tests` to include them) —
  the point is scoring the code under test, not the tests themselves.
- Exit codes: `0` success, `1` a function exceeded `-fail-over`, `2` usage
  or path/coverage-file error — check exit code in scripts/CI rather than
  parsing table output.
