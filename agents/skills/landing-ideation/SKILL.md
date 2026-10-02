---
name: landing-ideation
description: Use when asked to design or build a landing page, marketing site, or campaign page and no design direction has been chosen yet. Also use when a first landing-page draft came out generic (text left, graphic right, purple gradient) and needs a distinctive direction before rebuilding.
---

# Landing Ideation

Decide what the page should *be* before writing a line of markup. The output of this skill
is a **build prompt** that a coding pass (or another agent) executes. Pages built from a
one-line brief ("build me a landing page for my productivity app") all converge on the same
layout; pages built from a direction the user chose do not.

## When not to use

The user already has a brand system, mockups, or a direction they're committed to. Go
straight to building (impeccable) and skip this.

## Procedure

1. **Collect the brief.** Product, audience, the one action the page exists to drive, and any
   hard constraints (brand colours, existing fonts, framework). Don't ask for more than that.
2. **Go broad.** Ask the model for directions, shallow and many:

   > I want to come up with a bold, unique design language for <product>. List as many ideas
   > as you can, with short, high-level descriptions. Go broad, not deep.

   Aim for 15+. Include at least two of each kind:
   - **Reference worlds**: a specific medium, era or place the page lives in
     ("each section a still from a pixel-art game", "an isometric living 3D city where
     features are neighbourhoods").
   - **Rule-breaks**: a conventional rule deliberately violated and made to work
     ("radically asymmetric layout, dissonant colours, uncomfortable negative space;
     break all the rules but still make it look good").
   - **Material/physical**: paper, glass, signage, print, hardware.
3. **Visualise the shortlist.** Pick 3–4 with the user. For each, either generate a single
   hero mockup image (fal-genmedia) or a throwaway HTML hero. Note the user's gut reaction.
4. **Steer by taste.** Feedback is mostly subtractive at this stage. Capture what the user
   rejects as explicit constraints for the build prompt ("no skeuomorphism", "nothing that
   reads as SaaS", "not cream/beige"). If nothing lands, run seed-string-direction for a
   batch of directions the user didn't have to invent.
5. **Write the build prompt.** Ask the model to turn the chosen direction into the prompt
   that will build the page. It must contain, in this order:
   - the direction in one paragraph, naming the reference world or rule-break;
   - the page's single conversion goal and the sections that serve it;
   - the hero: image-led, not text-led; what the image is and what the one line of copy says;
   - typography and colour commitments (named fonts, named colour strategy);
   - motion: what responds to scroll or pointer, and what stays still;
   - the user's rejections from step 4 as hard constraints;
   - the final line: *"Then screenshot the page and run design-critic-loop until it scores 9."*
6. Hand the prompt to the build pass. Keep it unchanged across critic iterations so the
   critic is judging execution, not a moving target.

## Ambitious-prompt patterns

| Pattern | Shape | Example |
|---|---|---|
| Reference world | "set in <world>, where <features> are <things in that world>" | isometric 3D city, features as buildings |
| Medium transplant | "each section should feel like <a frame from medium>, yet function as a landing page" | pixel-art game stills |
| Rule-break | "<violate rule>, <violate rule>. Break all the rules but still make it look good." | asymmetry, dissonant type, negative space |
| Contradiction | "<quality A> yet <opposing quality B>" | brutalist yet warm |

The ideas that sound terrible are the ones worth visualising; the ones that sound safe are
the defaults you're trying to escape.

## Common mistakes

- Asking for depth too early. One fleshed-out idea costs more than fifteen shallow ones and
  anchors the user.
- Letting the model pick the favourite. The user's taste is the input the model doesn't have.
- Writing a build prompt that describes a layout ("hero, three features, pricing, footer")
  instead of a direction. Layout is the critic's job to fix; direction is the thing it can't
  invent.
- Skipping the visualisation step and choosing from text descriptions.
