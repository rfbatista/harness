---
name: video-motion-design
description: Use when a page needs motion that code can't produce convincingly: a hero object with real physics or materials (glass, liquid, cloth, smoke), a looping ambient graphic, or a scroll- or gesture-driven transition between visual states. Builds on fal-genmedia for the generation step.
---

# Video Motion Design

Use video models as a renderer. CSS and shaders handle layout motion well and physical
materials badly; a video model handles refraction, fluid, cloth and lighting for free. The
job is turning its output into something a web page can composite and control.

**REQUIRED SUB-SKILL:** fal-genmedia for every generation call. This skill covers what to
generate and how to use it, not how to run models.

## Two techniques

### A. Looping element with transparent background

For a hero object or ambient graphic layered over page content.

1. **Render over the page's own background colour**, not green or black. Refraction,
   shadows and glow are then baked in against the right colour; a green-screen pass
   produces fringes and wrong shadows.
   - Prompt the video model with the exact hex of the page background and a seamless loop
     (`"seamless loop, static camera, solid #<hex> background, object centred"`).
   - Image-to-video from a generated still gives far more control than text-to-video.
2. **Remove the background with a video-matting model** (search `genmedia models "video
   matting"` / `"background removal video"`), not chroma key. Output as WebM (VP9 with
   alpha) or an HEVC-with-alpha MOV for Safari; ship both.
3. **Composite in the page**: `<video autoplay muted loop playsinline>` positioned over
   the section. Keep the page background the same colour it was rendered against; if the
   section colour changes, re-render.
4. **Check frame by frame** in the browser (pause and step). Loop seams, matte flicker and
   edge halos are invisible at speed and obvious to the user.

### B. Scroll- or gesture-driven state transitions

For a product that morphs between states as the user scrolls, swipes, or hovers.

1. **Generate one keyframe image per state** (fal-genmedia, same model and seed where
   possible, same camera and lighting in the prompt). These are the anchors.
2. **Interpolate video between consecutive keyframes** with an image-to-video model that
   accepts a start and end frame (search `genmedia models "first last frame"` /
   `"frame interpolation"`). 2–4 seconds per transition.
3. **Chain the clips** so motion is continuous: extract the last frame of clip *n* and use
   it as the start frame of clip *n+1* instead of the original keyframe. The end state
   drifts slightly; that's acceptable, a visible cut is not.
4. **Scrub, don't play.** Load the chained video, set `currentTime` from scroll progress
   (or a pointer gesture) and never call `play()`. For smooth scrubbing, either pre-extract
   frames to an image sequence / sprite sheet, or encode with a keyframe every frame
   (`-g 1`) so seeking is instant.
5. **Respect `prefers-reduced-motion`**: show the final keyframe as a still.

## Budget and delivery

- Check `genmedia pricing` before each model; video is the expensive step of the whole
  landing-page flow. Generate stills first and get them approved before animating.
- Target ≤ 3 MB per hero asset. Serve a poster image, lazy-load anything below the fold.
- Keep the generated stills and prompts next to the asset; re-rendering without them means
  starting over.

## Common mistakes

- Green-screening a glass or liquid object. The material carries the background colour.
- Text-to-video for a hero element: no control over composition, no match to the page.
- Autoplaying a transition video instead of scrubbing it. Motion that ignores the user's
  input reads as a banner ad.
- Generating the whole sequence before the user has approved a single still.
- One loop that doesn't actually loop. Say "seamless loop" in the prompt and verify the
  first and last frames match.
