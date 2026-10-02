---
name: design-critic-loop
description: Use after a first visual draft of a landing page, hero, slide, or UI exists and needs to reach a studio-quality bar, or when the user says a design "looks fine but generic", "looks AI-made", or asks to iterate on a design until it's great. Requires the ability to screenshot the page.
---

# Design Critic Loop

Separate the eye from the hand. The agent that wrote the page can't see it the way a
reviewer does: it knows what it *meant*. A critic in a fresh context, given only a
screenshot, judges what's actually on the screen and scores it. Iterate until the score
holds.

## The loop

```
build/fix ──► screenshot ──► critic (fresh context, screenshot only) ──► score
   ▲                                                                      │
   └────────────── apply the critic's top gaps ◄───── score < target ─────┘
```

1. **Screenshot** the current page at desktop and one mobile width (dev-browser, or the
   browser tools available). Full page, not just the fold.
2. **Dispatch the critic** as a new subagent. Give it the screenshots and the prompt in
   `critic-prompt.md`, filled with the intended aesthetic from the build prompt. Give it
   **nothing else**: no code, no file paths, no previous critiques, no build history.
3. **Read the score and gaps.** Apply the gaps in the order the critic ranked them, in the
   builder's context. Don't argue with the critic in its context; if a gap is wrong for the
   brief, note why and move on.
4. **Repeat** with a fresh critic each time. Same prompt, same aesthetic statement, new
   screenshot.
5. **Stop** when the score is ≥ 9 on two consecutive critics, or after 6 rounds. If it's
   stuck at the same score for 3 rounds, the direction is the problem, not the execution:
   go back to landing-ideation.

## Why screenshot-only

The critic must not see the code, because code tells it what the author intended and it
starts grading intent. It must not see earlier critiques, because it will grade progress
("much better than last time") instead of distance from the bar. Every critic is a first
impression.

## Model split

Build with the model you'd normally use; critique with the strongest model available. The
critic reads one image and writes a few hundred words, so its cost is small and its taste
is the whole point.

## Scoring rubric the critic uses

| Score | Meaning |
|---|---|
| 9–10 | A top studio would ship this. Specific, confident, nothing to remove. |
| 7–8 | Competent. Clear direction, some generic or over-decorated areas. |
| 5–6 | Template-grade. Direction stated but not felt; several AI tells. |
| ≤ 4 | Default output. Layout and palette guessable from the category alone. |

## Common mistakes

- Passing the code "so the critic can suggest fixes". Fixes are the builder's job.
- Reusing one critic conversation across rounds. Its memory softens each round's verdict.
- Treating 8/10 as done because the user is tired. Say the score and let them decide.
- Changing the aesthetic statement between rounds to chase the score.
- Critiquing only the fold. Most tells (identical card grids, eyebrows on every section)
  live below it.
