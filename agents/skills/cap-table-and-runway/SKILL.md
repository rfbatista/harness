---
name: cap-table-and-runway
description: "Reason about cap table hygiene, equity dilution, burn rate, and runway — including when to start the next raise. Use when reviewing a cap table, modeling dilution for a future round, calculating burn/runway, or deciding how much to raise and when."
---
# Cap Table, Dilution, and Runway

## Domain Context

The cap table (capitalization table) is the single most important document in a startup —
it's the record of who owns what. Managing it well means three things:
1. Keeping it **structurally clean** — documented, properly vested, in real cap table
   software (not a stale spreadsheet)
2. **Modeling dilution proactively**, before each financing event, instead of reacting to
   it after the term sheet arrives
3. Connecting cap table decisions to the broader trade-offs the business actually faces —
   runway, headcount, growth rate

Most founders track *current* ownership but never model *future* dilution. The question to
always be able to answer: "If we raise the next round at price X, what do I — and the
team — own afterward?"

## When to Use

- Reviewing or cleaning up a cap table
- Modeling dilution ahead of a fundraise
- Calculating burn rate or runway
- Deciding how much to raise, and when to start
- Triggers: cap table, dilution, burn rate, runway, how much to raise, equity math

## Dilution

Dilution is the cost of venture-backed growth — the question isn't how to eliminate it,
it's how to give up as little as possible for each unit of real progress.

- **Dilutive funding** — equity in exchange for capital (priced rounds, SAFEs/notes that
  convert)
- **Non-dilutive funding** — debt, grants, revenue-based financing; doesn't touch
  ownership

Pre-revenue companies usually need dilutive capital. Post-revenue companies can often use
non-dilutive instruments to stretch runway between priced rounds without giving up more
equity than necessary.

## Burn Rate and Runway

```
Monthly burn = total monthly expenses − total monthly revenue
Runway (months) = cash in the bank ÷ monthly burn
```

- **Target runway: 18-24 months.** Enough time to hit the milestones that unlock the next
  round, adjust strategy if something isn't working, and raise from a position of
  strength rather than panic.
- **Start the next raise with ~6 months of runway left**, not when the number gets scary —
  fundraising processes routinely take longer than planned.
- **Raise only enough to hit the specific milestone** that unlocks the next round at a
  higher valuation. A bigger raise than needed just means more dilution for progress the
  business didn't actually need the money to make.

## Financial Model

Build a **24-36 month model focused on key drivers** (revenue growth rate, gross margin,
headcount cost, core unit economics) — not a 5-year model dressed up with false precision.
Investors read the 5-year numbers as a formality; the 24-36 month model is what actually
gets scrutinized.

## Prompt

You are a startup finance advisor helping a founder reason about $ARGUMENTS.

**Step 1: Establish the current state** — current cash, current monthly burn, current
runway in months. Flag if the cap table itself is stale, undocumented, or has unresolved
vesting/option issues.

**Step 2: Model the next round's dilution** — at a plausible price and raise size, what
does the founder/team own afterward? Show it as a number, not a vague "some dilution."

**Step 3: Check runway against the 18-24 month target** — if runway is under ~12 months,
flag that the raise conversation needs to start now, not later.

**Step 4: Size the raise to a milestone**, not a round number — what specific milestone
does this money need to buy before the next round?

**Step 5: Flag dilutive vs. non-dilutive options** — is there a non-dilutive lever
(revenue-based financing, a grant, a bridge note) that stretches runway without more
equity given up, given the company's stage?

## Output Process

1. Confirm current cash, burn, and runway
2. Model dilution for the proposed next round
3. Compare runway to the 18-24 month target and the "start raising at 6 months left" rule
4. Tie the raise size to a specific milestone, not a generic buffer
5. Note any non-dilutive options worth considering given the stage

## Notes

- A cap table with unclear/undocumented equity grants is a diligence red flag that slows
  down every future round — clean it up before it's urgent, not during a raise.
- This feeds directly into `fundraising-pitch-deck` (the ask slide) and `investor-updates`
  (the cash/burn/runway metrics line).

---

### Further Reading

- [Cap Table Management: Complete Guide for Startups](https://www.re-cap.com/blog/cap-table-management-startup)
- [Equity Dilution Explained: A Founder's Guide](https://www.crv.com/content/equity-dilution)
- [Cap table management for startups: a CFO-level guide](https://www.fiscallion.io/blog/cap-table-management-for-startups)
- [How to Manage Startup Equity Dilution: A Founder's Guide](https://startupfundraising.com/library/articles/from-founder-to-minority-equity-dilution-mistakes-to-watch-out-for-13_VkB)
