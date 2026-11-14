# ADR document format

One file per decision in `project/docs/` of your virtual shell, titled
`ADR NNNN: <short title>` in its first heading, numbered one past the highest
existing ADR (`grep -h '^# ADR' project/docs/*`). Name the file after it:
`adr-NNNN-<slug>.md`.

## Template

```markdown
# ADR NNNN: <short title>

<One to three sentences: the context, what was decided, and why.>
```

That is all most ADRs need. The value is recording *that* a decision was made and
*why*, not filling in sections. Add these only when they earn their place:

- `## Status` — proposed, accepted, deprecated, or superseded by ADR NNNN; useful when a decision is revisited.
- `## Considered options` — only when the rejected alternatives are worth remembering.
- `## Consequences` — only for downstream effects that are not obvious.

## When to offer one

All three must hold:

1. **Hard to reverse** — changing your mind later costs something real.
2. **Surprising without context** — a future reader would wonder why it was done this way.
3. **A real trade-off** — there were genuine alternatives and one was picked for reasons.

What qualifies: the architectural shape; integration patterns between contexts
("Ordering and Billing talk through domain events, not synchronous calls");
technology choices with lock-in; boundary and scope decisions, including the explicit
no's; deliberate deviations from the obvious path; constraints the code does not show;
rejected alternatives whose rejection is not obvious.
