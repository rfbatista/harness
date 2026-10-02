---
name: balanced-scorecard
description: "Build a Balanced Scorecard that connects strategy to measurable outcomes across financial, customer, internal process, and learning/growth perspectives. Use when strategy exists but isn't tied to metrics, or when a single financial number is being used as the whole performance picture."
---
# Balanced Scorecard

## Domain Context

A Balanced Scorecard is a management system that keeps an organization focused on
big-picture strategic goals by measuring performance across **four perspectives**, not
just the financial one. Financial results are a lagging indicator — by the time they move,
it's often too late to correct course. The other three perspectives are the leading
indicators that explain *why* the financial numbers will move next.

## When to Use

- Strategy exists on paper but isn't connected to any metric that tracks it
- Leadership is managing off a single financial number (revenue, margin) with no visibility
  into what's driving it
- Setting up quarterly/annual strategic review cadence
- Triggers: balanced scorecard, BSC, strategy metrics, performance across perspectives

## The Four Perspectives

1. **Financial** — how do we look to shareholders/owners? (revenue, margin, cash
   generation)
2. **Customer** — how do customers see us? (satisfaction, retention, NPS, market share in
   target segment)
3. **Internal Process** — what must we excel at internally to satisfy customers and
   shareholders? (cycle time, quality, operational efficiency)
4. **Learning & Growth** — can we continue to improve and create value? (employee skills,
   systems/infrastructure, culture, ability to adapt)

The perspectives are causally linked, bottom to top: Learning & Growth enables better
Internal Process, which produces better Customer outcomes, which drives Financial results.
A scorecard that only has Financial metrics is measuring the outcome with no visibility
into the cause.

## Prompt

You are a strategy advisor building a Balanced Scorecard for $ARGUMENTS.

**Step 1: State the strategy in one sentence** — if the strategy can't be stated simply,
the scorecard will just measure activity, not strategy.

**Step 2: For each perspective, pick 2-4 metrics** — not more; a scorecard with 30 metrics
across four perspectives isn't focused, it's a dashboard.
- Financial: what proves the strategy is paying off financially?
- Customer: what proves customers are getting more value / choosing us more?
- Internal Process: what internal capability has to work for the customer promise to hold?
- Learning & Growth: what has to be true about the team/systems for the process to keep
  improving?

**Step 3: Draw the causal chain** — for each Financial metric, trace back which Customer
metric drives it, which Internal Process metric drives that, which Learning & Growth
metric drives that. If a metric doesn't sit in a chain, question whether it belongs on the
scorecard.

**Step 4: Set targets and owners** — every metric needs a target and a named owner, or it
will not get reviewed.

## Output Process

1. Confirm the one-sentence strategy statement
2. Draft 2-4 metrics per perspective
3. Map the causal chain from Learning & Growth up to Financial
4. Assign targets and owners
5. Set the review cadence (typically quarterly)

## Notes

- This pairs naturally with `north-star-metric` — the North Star is usually the anchor
  Customer-perspective metric, and Balanced Scorecard is the fuller picture around it.
- A scorecard that never changes over years is a sign the strategy hasn't been
  re-examined, not a sign of stability.

---

### Further Reading

- [What Is The Balanced Scorecard? — Complete Guide](https://fourweekmba.com/balanced-scorecard/)
