# ADR document format

One project document per decision, titled `ADR: NNNN <short title>`, numbered one
past the highest existing `ADR:` title in `list_project_documents`. A complete HTML
page, like every task document.

## Template

```html
<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>ADR: NNNN <short title></title>
<style>
  :root { color-scheme: light dark; }
  body { font: 15px/1.5 system-ui, sans-serif; max-width: 72ch; margin: 2rem auto; padding: 0 1rem; }
  h1, h2 { line-height: 1.25; }
</style>
</head>
<body>
<h1>ADR NNNN: &lt;short title&gt;</h1>
<p>&lt;One to three sentences: the context, what was decided, and why.&gt;</p>
</body>
</html>
```

That is all most ADRs need. The value is recording *that* a decision was made and
*why*, not filling in sections. Add these only when they earn their place:

- `<h2>Status</h2>` — proposed, accepted, deprecated, or superseded by ADR NNNN; useful when a decision is revisited.
- `<h2>Considered options</h2>` — only when the rejected alternatives are worth remembering.
- `<h2>Consequences</h2>` — only for downstream effects that are not obvious.

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
