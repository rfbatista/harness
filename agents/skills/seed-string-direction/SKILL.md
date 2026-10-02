---
name: seed-string-direction
description: Use when generating several visual directions for a page, component, or brand and the outputs keep converging on the same defaults, or when the user has no opinion yet and wants options they didn't have to invent. Works for landing pages, hero sections, slide themes, and palettes.
---

# Seed-String Direction

Replace the model's default taste with a random anchor. A random alphanumeric string has no
aesthetic, so the model has to *read* one into it; each string produces a different reading,
and the readings don't cluster around the training-data median the way unanchored prompts do.

## Procedure

1. Generate the seed with the bundled script; never ask the model to invent one (it will
   pick something pretty):

   ```bash
   bash scripts/seed.sh          # one 64-char string
   bash scripts/seed.sh 4        # four strings, one per variant
   ```

2. Give the build or ideation prompt this procedure, verbatim, with the string substituted:

   > Follow this procedure:
   > 1. Here is a random alphanumeric string: `<SEED>`.
   > 2. Define the creative direction (colour scheme, layout, typography, imagery, motion)
   >    based on the string. Look beyond the surface for subpatterns, repeated characters,
   >    special numbers, anything that inspires you.
   > 3. Use your judgment to bring this direction to life and make it look great.
   > 4. Don't reveal the string in the design, and don't mention it in the output.

3. For a batch, run one variant per seed in separate contexts so they can't converge.
   Screenshot each; let the user pick. The winner's direction goes into landing-ideation
   step 5 as the build prompt.

4. Record the seed next to the chosen direction (in the build prompt's header or a comment).
   It's the only way to regenerate a sibling of a direction later.

## When not to use

- The user already has a direction or a brand system. Seeds generate variety; they don't
  respect constraints.
- A single polished output is wanted and there's no time to pick. One seed is a coin flip.

## Common mistakes

- Letting the string leak into the design (as decorative text, a hash in the footer, a
  "seed" label). The instruction in step 2 has to be present every time.
- Asking the model to also explain how it read the string. The explanation becomes the
  design; keep it hidden.
- Re-using one seed for all variants. Same seed, same reading.
