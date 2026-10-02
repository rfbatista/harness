---
name: fal-genmedia
description: Generate or edit images and video using fal.ai's models via the `genmedia` CLI — text-to-image, image-to-image, text-to-video, image-to-video. Use when asked to generate, create, mock up, or produce an image or video asset (marketing creative, product mockups, social/landing page visuals, pitch-deck imagery, demo video clips). Covers model discovery, running a model, async video jobs, and downloading results.
---

# fal.ai Media Generation (genmedia CLI)

Generate images and video by driving fal.ai's 1,000+ hosted models through the
`genmedia` CLI — never call fal's raw HTTP/queue API directly. `genmedia` is
fal's own agent-first client: it already handles auth, model schemas, async
polling, and file downloads, and switches to structured JSON automatically
when piped (or with `--json`), which is what makes it usable from an agent
loop instead of a human terminal.

## When to Use

- Asked to generate/create/mock up an image (marketing creative, social
  graphics, landing-page visuals, pitch-deck imagery, product mockups,
  moodboards)
- Asked to generate a short video clip or animate an existing image
- Need to discover what fal model fits a task, or what a model costs before
  running it
- Triggers: generate an image, generate a video, image generation, video
  generation, fal.ai, text-to-image, text-to-video, image-to-video

## Setup

`genmedia` needs a `FAL_KEY` (from fal.ai/dashboard/keys) available as an
environment variable — never hardcode a key in a command or file. In this
repo, secrets live in the gitignored `.env` sourced by `zsh/.zshrc`; add
`FAL_KEY=...` there rather than exporting it ad hoc.

```bash
curl https://genmedia.sh/install -fsS | bash   # installs to ~/.genmedia, adds to PATH
genmedia setup --non-interactive --api-key "$FAL_KEY"   # non-interactive, agent-safe
```

If `genmedia` isn't on `PATH` when a session starts, run the install command
first — don't fall back to raw `curl`/HTTP calls against fal's API.

## Core Commands

| Command | Purpose |
|---|---|
| `genmedia models "<query>" [--category text-to-image\|text-to-video\|...] [--json]` | Search the model catalog by keyword/category |
| `genmedia schema <endpoint-id> [--format openapi]` | Show a model's exact input/output parameters |
| `genmedia run <endpoint-id> --<param> <value>... [--download [template]] [--logs] [--async]` | Execute a model |
| `genmedia status <endpoint-id> <request-id> [--result] [--logs] [--cancel] [--download]` | Check/collect an async job |
| `genmedia upload <path-or-url>` | Push a local file (or URL) to fal's CDN, returns a `cdn_url` for image-to-image/image-to-video inputs |
| `genmedia pricing <endpoint-id>` | Show cost before running an expensive model |
| `genmedia gallery open current` / `genmedia gallery list` | Review this session's generated outputs (`~/.genmedia/gallery/sessions/`) |

Parameter flags on `run`/`status` match the model's own schema field names
exactly — run `genmedia run <endpoint-id> --help` (or `genmedia schema
<endpoint-id>`) when unsure what a model accepts, rather than guessing flag
names.

## Workflow

1. **Know the cost first** for anything beyond a cheap/fast image model:
   `genmedia pricing <endpoint-id>`.
2. **Pick a model.** A bare prompt with no endpoint id lets `genmedia`
   auto-select a suitable model — fine for a quick draft, but pin an explicit
   `<endpoint-id>` (from `genmedia models "..."`) for anything going into a
   deliverable, so results are reproducible.
3. **Images (usually synchronous):**
   ```bash
   genmedia run fal-ai/flux/dev --prompt "a cat" --download
   ```
   `--download` saves the output file(s) locally using the `{index}`/`{name}`/
   `{ext}`/`{request_id}` template placeholders.
4. **Image-to-image / image-to-video inputs:** upload the source file first to
   get a `cdn_url`, then pass that as the model's image parameter:
   ```bash
   genmedia upload ./source.png
   genmedia run fal-ai/<model> --image_url <cdn_url> --prompt "..."
   ```
5. **Video (usually async — can take minutes):**
   ```bash
   genmedia run fal-ai/kling-video/v3/pro/text-to-video --prompt "a cat playing piano" --async
   # -> prints a request id
   genmedia status fal-ai/kling-video/v3/pro/text-to-video <request-id> --result --download
   ```
   Don't block synchronously waiting on a long video job inline — submit with
   `--async`, tell the user the request id, and check back with `status`
   rather than polling in a tight loop.
6. **Piping/scripting:** add `--json` (or just pipe) to get structured output
   for parsing, e.g. `genmedia models "text to video" --json | jq '.models[]'`.

## Using generated media inside a page

When the output goes into a landing page or UI rather than being the deliverable itself:

- **Replace, don't decorate.** Generated images earn their place by standing in for a
  gradient, abstract shape or stock photo that was carrying a section. One strong image per
  section beats several small illustrations.
- **Hold one style across the set.** Pin the endpoint id, reuse the seed where the model
  exposes one, and keep a shared style suffix in every prompt (lighting, palette, medium).
  Generate the hero first, get it approved, then derive the rest from it with
  image-to-image.
- **Render against the page.** Give the prompt the page's background hex so lighting and
  shadows match; for cut-out objects, use a background-removal model afterwards rather than
  prompting for "transparent background".
- **Layer with code.** Generated stills work under CSS blend modes, masks and WebGL shaders
  (grain, displacement, parallax). Check the composite in a browser frame by frame, not from
  the raw file.
- **Motion:** for anything that moves, see the video-motion-design skill.

## Notes

- `genmedia` targets *fal-hosted* models specifically — for any other
  image/video generation provider, this skill doesn't apply.
- Prefer an explicit `<endpoint-id>` over auto-selection whenever the output
  needs to be reproducible or reviewed (marketing creative, pitch decks) —
  auto-selection is a convenience for quick exploration, not a pinned choice.
- `GENMEDIA_NO_GALLERY=1` disables per-session gallery recording;
  `GENMEDIA_NO_UPDATE=1` disables auto-update checks — set either in a
  scripted/CI context if the extra I/O isn't wanted.

---

### Further Reading

- [genmedia CLI overview](https://fal.ai/docs/documentation/setting-up/genmedia)
- [fal API key setup](https://fal.ai/docs/documentation/setting-up/authentication)
- [fal Model APIs overview](https://fal.ai/docs/documentation/model-apis/overview)
