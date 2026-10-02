---
name: remove-ai-tells
description: Use as the last pass before a landing page, marketing page, or UI ships, or when someone says a design "looks AI-generated", "looks like every other SaaS page", or asks to make it cleaner, quieter, or more intentional. Also use when the design-critic-loop plateaus just under its target.
---

# Remove AI Tells

Finishing is subtraction. Generated designs fail by adding: a glow here, a gradient there, a
label explaining what the button already says. Putting less on the screen communicates more,
because the user's attention isn't spent on decoration. This pass removes everything that
has no job.

## The pass

Walk the page top to bottom. For every element ask one question: **what breaks if this is
removed?** If the answer is "nothing", remove it. If the answer is "it looks emptier", that
is not a break; let it be emptier, then fix the spacing.

Then walk it again against the checklist below. Each row is a tell; remove or replace, don't
tone down.

| Tell | Replace with |
|---|---|
| Purple or multi-stop gradient anywhere | one solid colour from the palette |
| Text left, graphic right, CTA below, nav on top | an image-led hero, or an asymmetric one |
| Glow, halo or blurred shadow behind UI elements | nothing; a 1px border if separation is needed |
| Random words highlighted in a brand colour | plain text; emphasis by weight or size only |
| Custom buttons, inputs or selects that behave worse than native | native controls, restyled minimally |
| A label, caption or sub-headline restating the element it sits on | delete it |
| Containers (cards, panels, boxes) that group one thing | remove the container, keep the spacing |
| Icon + heading + text card grid, every card the same | a list, a single image, or varied layouts |
| Small uppercase tracked eyebrow above every section | none, or one deliberate kicker on the page |
| Numbered sections (01 / 02 / 03) that aren't a sequence | plain headings |
| Gradient text, glass cards, side-stripe borders | solid text, flat surfaces, full borders |
| Cream/beige/sand body background | true off-white at chroma 0, or a committed brand colour |
| Three-adjective copy ("fast, simple, powerful") | one concrete claim |
| Decorative background shapes, blobs, grids, dot patterns | the background colour |

**REQUIRED BACKGROUND:** impeccable's "Absolute bans" and "AI slop test" cover the same
ground at design time; this pass is the audit after the fact.

## Then tighten

Once things are gone, what remains is usually too large and too loose for the space:

- Reduce type sizes a step; display headings rarely need the clamp max they were given.
- Tighten vertical rhythm. Whitespace that was holding decoration apart is now just gaps.
- Prefer one image that carries the section over three that illustrate it.
- Re-screenshot and run design-critic-loop once more. Removal usually lifts the score by a
  point on its own.

## Common mistakes

- Making tells subtler instead of removing them (a fainter glow, a two-stop gradient).
- Removing function along with decoration: a border that was the only focus indicator, a
  label that was the only accessible name. Check keyboard focus and screen-reader names
  after the pass.
- Replacing one tell with another (eyebrows become numbered sections, cards become
  glass panels).
- Running this pass before the direction is settled. Restraint on a generic design yields a
  cleaner generic design.
