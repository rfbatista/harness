---
name: mckinsey-7s-framework
description: "Audit organizational alignment across the McKinsey 7S elements (Strategy, Structure, Systems, Shared Values, Skills, Style, Staff) to find where a strategy is failing because the organization around it isn't aligned. Use during a major reorg, after a strategy pivot, or when execution keeps stalling despite a clear strategy."
---
# McKinsey 7S Framework

## Domain Context

The 7S framework aligns seven internal elements to diagnose organizational
effectiveness. It's split into two categories:

- **Hard elements** (easier to define and directly control): **Strategy**, **Structure**,
  **Systems**
- **Soft elements** (harder to define, more cultural): **Shared Values**, **Skills**,
  **Style**, **Staff**

The core insight: a good strategy fails in execution more often because the organization
around it isn't aligned than because the strategy itself was wrong. All seven elements
need to reinforce each other — changing one (a new strategy, a reorg) without checking the
other six is how "the strategy was right but execution failed" happens.

## When to Use

- Strategy just changed (pivot, new market, new business model) and execution needs to
  follow
- Going through a reorg and need to check more than just the org chart
- Execution keeps stalling despite a strategy that looks clear on paper
- Triggers: McKinsey 7S, organizational alignment, why isn't execution working, reorg
  checklist

## The Seven Elements

1. **Strategy** — the plan to build and sustain competitive advantage
2. **Structure** — how the organization is divided (org chart, reporting lines, business
   units)
3. **Systems** — the processes and procedures that get work done day to day (planning,
   budgeting, reporting, tooling)
4. **Shared Values** — the core beliefs and culture that guide behavior (the center of the
   model — everything else should align to this)
5. **Skills** — the actual capabilities the organization has (vs. the capabilities the
   strategy requires)
6. **Style** — how leadership actually behaves and makes decisions, not how it's described
   on paper
7. **Staff** — the people: headcount, roles, how they're developed and retained

## Application Order

Start with **Shared Values** (the center), then examine the **hard elements** (Strategy,
Structure, Systems), then the remaining **soft elements** (Skills, Style, Staff). Look for
misalignment at each step, not just a checklist pass — a new Strategy that assumes Skills
the org doesn't have, or a Structure that contradicts the Shared Values, is where execution
actually breaks.

## Prompt

You are an organizational strategist running a 7S audit for $ARGUMENTS.

**Step 1: State Shared Values** — what does this organization actually believe/prioritize
in practice (not the values poster)?

**Step 2: Describe each hard element** — current Strategy, current Structure, current
Systems.

**Step 3: Describe each soft element** — current Skills (capabilities that exist), current
Style (how decisions actually get made), current Staff (who's here, in what roles).

**Step 4: Find misalignments** — for each pair of elements, ask if they reinforce or
contradict each other. Flag every contradiction explicitly (e.g. "Strategy assumes rapid
experimentation, but Systems require multi-week approval cycles").

**Step 5: Recommend the minimum set of changes** that would bring the misaligned elements
back into alignment with Strategy and Shared Values — don't recommend changing everything
at once.

## Output Process

1. Document all seven elements as they currently stand
2. Cross-check every pair for contradiction
3. Rank misalignments by how much they're actually blocking execution
4. Recommend the smallest set of changes that resolves the highest-impact misalignments

## Notes

- This is a diagnostic tool, not a strategy-generation tool — use it after a strategy
  exists (see `product-strategy`/`business-model`) to check whether the organization can
  actually execute it.
- A reorg that only changes Structure without checking Systems, Skills, and Style is the
  most common way 7S misalignment gets introduced, not resolved.

---

### Further Reading

- [The McKinsey 7-S Model Framework, Explained](https://whatfix.com/blog/mckinsey-7s-model/)
- [McKinsey 7S Model: The 7S Framework Explained](https://strategicmanagementinsight.com/tools/mckinsey-7s-model-framework/)
