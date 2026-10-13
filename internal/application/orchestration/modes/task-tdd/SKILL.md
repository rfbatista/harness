---
name: task-tdd
description: Use in a harness implement-mode session (or an implementer sub-agent of one) to build a ticket test first — the red → green loop at the seams the Spec task document agreed, with what makes a test worth keeping and the anti-patterns to avoid. Adapted from mattpocock/skills tdd.
---

# Test-Driven Development

TDD is the red → green loop. This skill is the reference that makes that loop produce
tests worth keeping: what a good test is, where tests go, the anti-patterns, and the
rules of the loop. Every section applies on every cycle: consult them before and
during the loop, not after.

Name tests and interfaces in the project's language: the `Glossary: <project>` project
document (find it with `mcp__task__search_documents`), and respect the `ADR:` documents
in the area you touch.

## What a good test is

Tests verify behaviour through public interfaces, not implementation details. Code can
change entirely; tests shouldn't. A good test reads like a specification: "user can
checkout with valid cart" tells you exactly what capability exists, and it survives
refactors because it doesn't care about internal structure.

See [tests.md](tests.md) for examples and [mocking.md](mocking.md) for mocking
guidelines.

## Seams: where tests go

A **seam** is the public boundary you test at: the interface where you observe
behaviour without reaching inside. Tests live at seams, never against internals.

**Test only at the agreed seams.** The person agreed them with the architect; they are
in the **Testing decisions** section of your Spec task document. Read that section
before writing any test. If a ticket cannot be tested at any of them, do not invent
one: ask the architect with `mcp__task__message_architect` kind question, giving the
seam you would add and a line on what it catches and what it misses. (An implementer
sub-agent asks the session that started it instead.)

## Anti-patterns

- **Implementation-coupled**: mocks internal collaborators, tests private methods, or
  verifies through a side channel (querying the database instead of using the
  interface). The tell: the test breaks when you refactor but behaviour hasn't changed.
- **Tautological**: the assertion recomputes the expected value the way the code does
  (`expect(add(a, b)).toBe(a + b)`, a snapshot derived by hand the same way, a constant
  asserted equal to itself), so it passes by construction and can never disagree with
  the code. Expected values come from an independent source of truth: a known-good
  literal, a worked example, the spec's user stories and acceptance criteria.
- **Horizontal slicing**: writing all tests first, then all implementation. Bulk tests
  verify *imagined* behaviour. Work in **vertical slices** instead: one test → one
  implementation → repeat, each test a **tracer bullet** that responds to what the last
  cycle taught you.

## Rules of the loop

- **Red before green.** Write the failing test first and watch it fail for the right
  reason, then write only enough code to pass it. No speculative features.
- **One slice at a time.** One seam, one test, one minimal implementation per cycle.
- **Refactoring is not part of the loop.** It belongs to the review stage
  (`task-code-review`), not the red → green cycle.
- **The ticket is done** when every acceptance criterion has a passing test at a seam
  and the repository's checks pass. Commit then.
