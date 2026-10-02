---
name: bcg-growth-share-matrix
description: "Categorize products, business units, or lines into the BCG Growth-Share Matrix (Stars, Cash Cows, Question Marks, Dogs) to guide investment, harvest, and divest decisions across a portfolio. Use when a company has more than one product/line and needs to decide where to invest, hold, or cut."
---
# BCG Growth-Share Matrix

## Domain Context

Developed by Bruce Henderson of Boston Consulting Group in 1968, the Growth-Share Matrix
places each product/business unit on a 2x2 grid: **market growth rate** (vertical) against
**relative market share** (horizontal). It's a portfolio-allocation tool, not a
single-product strategy tool — it only makes sense once there's more than one thing to
compare investment across.

## When to Use

- The company has more than one product line, market, or business unit
- Deciding where to invest more, where to hold steady, and what to cut
- A founder/leadership team is spreading resources evenly across products instead of by
  where they actually pay off
- Triggers: BCG matrix, growth-share matrix, stars cash cows dogs, portfolio strategy,
  where to invest

## The Four Quadrants

|  | High Market Share | Low Market Share |
|---|---|---|
| **High Market Growth** | **Star** — invest to maintain/grow position | **Question Mark** — invest selectively or exit |
| **Low Market Growth** | **Cash Cow** — harvest, reinvest cash elsewhere | **Dog** — divest or reposition |

- **Stars** — high growth, high share. Consume cash to defend/grow position, but are the
  future Cash Cows once growth slows. Default strategy: invest.
- **Cash Cows** — low growth, high share. Generate more cash than they need; fund
  everything else from here. Default strategy: harvest, don't over-invest.
- **Question Marks** — high growth, low share. Could become Stars with investment, or
  become Dogs if left unfunded. Default strategy: pick a few to back hard, don't spread
  thin across all of them.
- **Dogs** — low growth, low share. Rarely worth the resources they consume. Default
  strategy: divest, sunset, or reposition into a different quadrant.

## Prompt

You are a portfolio strategist applying the BCG Growth-Share Matrix to $ARGUMENTS.

**Step 1: List every product/line/business unit** to be plotted.

**Step 2: Estimate market growth rate** for each one's market (roughly: is this a growing,
flat, or shrinking market?).

**Step 3: Estimate relative market share** for each one (share relative to the largest
competitor in that space, not just absolute share).

**Step 4: Plot each into a quadrant** and apply the default strategy — flag any mismatch
between current resourcing and the quadrant (e.g. a Dog getting Star-level investment, or
a Question Mark starved of the funding it needs to become a Star).

**Step 5: Recommend reallocation** — where should Cash Cow proceeds actually go, and which
Question Marks earn that investment vs. which should be cut loose?

## Output Process

1. Enumerate the portfolio
2. Plot growth rate × relative share per item
3. Assign quadrants
4. Compare current investment levels against the quadrant's default strategy
5. Recommend specific reallocation moves

## Notes

- This is a simplification, not a full valuation model — use it to structure the
  conversation about where resources go, not as the sole justification for a cut decision.
- A portfolio that's all Cash Cows and no Question Marks/Stars has no future growth engine
  funded — that's a strategic gap worth surfacing even if every current number looks fine.

---

### Further Reading

- [BCG Matrix: Complete Guide with Examples](https://fourweekmba.com/bcg-matrix/)
- [What Is the Growth Share Matrix?](https://www.bcg.com/about/overview/our-history/growth-share-matrix)
