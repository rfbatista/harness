---
name: clarifying-tickets
description: Use when a ticket, issue, or spec has been assigned and is underspecified, ambiguous, or sits in a domain you do not yet understand — before planning or implementing it. Triggers include "clarify this task", "help me understand this ticket", "explain the problem and a possible solution", "build me documentation for this", or a spec whose assumptions nobody has checked against the codebase yet.
---

# Clarifying Tickets

## Overview

A ticket is written by someone who knows the product. You have to build it in a codebase that already made decisions the ticket never saw. A **clarification brief** closes that gap: it teaches the domain, names every place the ticket and the code disagree, sketches a solution shape, and ends with the decisions only the ticket author can make.

**Core principle: the brief's content is the delta, not the ticket.** Anything the reader could get by opening the ticket does not belong in the brief. A brief that restates the spec back at its author is worth nothing to them.

## When to Use

- A ticket landed in your queue and you are not certain what it means
- The spec was written by a PM or designer without reading the code
- The domain has vocabulary you cannot yet use confidently (billing, tax, clinical, legal, payments)
- Someone asks you to "understand the problem" or explain a task before work starts
- You are about to estimate or plan something whose assumptions are unverified

**When NOT to use:** the ticket is clear and grounded — just build it. The decisions are already answered — that is planning, use `planning-and-task-breakdown`. You are debugging existing behaviour — that is `systematic-debugging`.

## Investigate First

Read the ticket, then read the code it lands in — models and migrations, the service that writes the relevant rows, the routes and screens that would host it, and what already exists to reuse. Every claim in the brief carries a `file:line` so the reader can check it.

Investigation is usually the easy part. **Shaping the output is where briefs go wrong**, so spend your care there.

## The Brief Is Seven Parts, In This Order

Write each part to its contract. Nothing else goes in the document.

| # | Part | Contract |
|---|------|----------|
| 1 | **Verdict** | Two or three sentences: what this feature is, and the single thing that makes it harder than it looks. |
| 2 | **Domain concepts** | **REQUIRED.** The vocabulary a reader needs to follow the rest, each term glossed plainly and anchored to where it lives in the code. Include the lifecycle the domain revolves around as a diagram. Keep terms of art in their original language and gloss them. |
| 3 | **The ask** | One paragraph restating the ticket faithfully. One paragraph only — this is the part the reader already knows. |
| 4 | **Findings** | The delta. One short entry each: what the ticket assumes, what the code actually holds, `file:line`, and why it matters to a user. Order by cost, not by discovery. |
| 5 | **Contradictions** | Places the ticket disagrees with *itself* — the same state named twice, an acceptance criterion the body does not describe, a sentence that admits two readings. These are free wins and authors are always grateful for them. |
| 6 | **Solution shape** | One diagram and a build-vs-reuse table: what is genuinely new, what already exists and can be reused, and the single design commitment worth making up front. That commitment is a *principle* — snapshot at write vs. join at read — never a schema or an endpoint list. |
| 7 | **Decisions** | The questions only the ticket author can answer. Each one: the question, why it blocks or changes the work, and a **recommended default** — so the reply can be a yes instead of an essay. |

**Part 2 is the part that gets dropped.** Without it the findings are unreadable to anyone who does not already know the domain — which includes you, three weeks from now, and every reviewer of the PR.

### Three kinds of finding

- **Missing** — the ticket needs something the code has no source for.
- **Contradicted** — the code already does something incompatible with what the ticket assumes.
- **Already built** — part or all of the feature ships today and the author does not know. **Lead with this one whenever it appears.** It can turn a ticket from "build" into "extend", and no other finding saves as much work.

When findings run long, merge anything that shares a single fix into one entry and drop anything that would change neither a decision nor a line of code. A reader who stops halfway has read the half that mattered only if you ordered by cost.

One root cause can produce an entry in both part 4 and part 5 — when a word means two things, the collision is a finding and the ambiguity it creates is a contradiction. That is expected; do not force it into one place.

## Where the Brief Ends

**The last section is Decisions. The document ends there.**

Once the decisions come back answered, that is a new task, and it is planning — a different skill. A brief that already contains the schema, the PR slicing, and the branch name has answered the open questions on the author's behalf, which is exactly what it was written to avoid.

## Deliverable

Unless the caller names a destination or a format, publish the brief as an artifact — the audience is the ticket author and the reviewers, and a brief nobody can open is a brief nobody reads. When the caller does name one, follow it.

Stamp the brief with the repo and the commit you read. Every `file:line` in it goes stale silently otherwise.

Diagrams show a mechanism the prose cannot: the naming collision, the state that gets overwritten, the feeds that converge. Draw them in whatever the destination renders — inline SVG in an artifact, mermaid in markdown. A labelled box that repeats its own caption is decoration; cut it.

Target the whole brief at what a reader will actually finish. Findings are short entries, not nested subsections.

## Common Mistakes

| Mistake | Fix |
|---------|-----|
| Restating the ticket in prettier sections | Cut anything the reader gets from the ticket itself. The brief is parts 2, 4, 5 and 7. |
| Skipping domain concepts because you learned them an hour ago | Part 2 is required. You needed it; so does everyone else. |
| Growing into an implementation plan — schema, PR list, estimates, branch names | Stop at Decisions. |
| Telling the engineer to go create tasks and start coding | The next step is getting answers, not starting work. |
| Reporting "this is missing" without checking whether it already ships | Search for the feature's own words in the repo before calling anything new. |
| Findings stated as generic risk ("the status mapping may be complex") | A finding names a file, a line, and what breaks for a user. |
| Ten unranked open questions | Order by what changes the work. Give every one a recommended default. |
| Diagrams that label components instead of showing flow | Draw the mechanism the argument turns on. |
