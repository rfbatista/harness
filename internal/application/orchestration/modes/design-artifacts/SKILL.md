---
name: design-artifacts
description: Use in a harness task session started in design mode, or whenever the task is to design components, screens, images or videos for a person who is watching the web UI. The designer produces self-contained HTML, images and videos in the worktree and publishes each one to the Design tab with publish_artifact as it is made, re-publishing after every change, while the conversation continues in the terminal.
---

# Design Artifacts

## Overview

You are the designer for the task this session was spawned into. The person
talks to you here, in the terminal, and **watches your work appear in the
Design tab of the web UI**. Nothing you make is visible there until you
publish it; everything you publish appears or refreshes within a second.

**Core loop:** make a change in the worktree → `publish_artifact` → tell the
person in one line what changed → listen.

## Hard Rules

- **Publish after every change.** A component you edited but did not re-publish
  is a component the person cannot see. Re-publishing the same path keeps the
  same card and bumps its revision; put what changed in `note`.
- **Self-contained HTML.** One file per component or screen, inline CSS and
  JS, no build step, no external network (no CDN scripts, fonts or images).
  Sibling files next to the page (`./style.css`, `./hero.png`) are served too,
  but prefer inline. Pages render inside a sandbox: scripts run, but nothing
  can reach the network or the harness API.
- **Everything lives under `design/` in the worktree.** `design/<component>.html`,
  `design/media/<name>.png|.mp4`. Files outside the worktree cannot be
  published; the home directory and tool galleries are invisible to the UI.
- **Never paste HTML into the terminal.** Say "Published *Pricing card* to the
  Design tab (rev 3): tighter spacing, hover state." and stop.
- **Commit nothing unless asked.** The worktree is the working surface.
- Titles and notes are shown as plain text; keep them short and human.

## Tools

| Tool | Use it to |
|---|---|
| `mcp__task__publish_artifact` | Show a worktree file (`path`) or a loopback dev server (`url`) in the Design tab. `title` required; `note` says what changed; `kind` only if inference would be wrong (`page`, `image`, `video`, `url`, `file`). |
| `mcp__task__list_task_artifacts` | See what this and earlier sessions already published before making more. |
| `mcp__task__unpublish_artifact` | Take one of your own cards down (the file stays). |
| `mcp__task__get_task` / `read_task_document` | The brief, and any design notes or specs earlier sessions left. |

`publish_artifact` returns `{artifact_id, revision, kind, view_url}`. Its
errors name the rule you broke: `ARTIFACT_PATH_OUTSIDE_WORKTREE` (move the
file under the worktree), `ARTIFACT_NOT_FOUND` (path wrong or a directory),
`ARTIFACT_URL_NOT_LOCAL` (only `http://localhost:…` / `http://127.0.0.1:…`),
`ARTIFACT_TOO_LARGE` (the message states the cap; shorten or compress),
`ARTIFACT_KIND_MISMATCH` (omit `kind`).

## Components and screens

1. Read the task and `list_task_artifacts`.
2. Write `design/<name>.html`: a complete document, inline `<style>` and
   `<script>`, realistic copy, the states the person asked for (default,
   hover, empty, error…) visible on one page or as separate files.
3. `publish_artifact` with `path: "design/<name>.html"`, a short `title`, a
   one-line `note`.
4. One line in the terminal. Then iterate on feedback: edit, re-publish, one line.

## Images and videos

Use the agent's media skills (for example `fal-genmedia`'s `genmedia run …
--download`). **Save the output into the worktree**, e.g.
`--download design/media/{name}` or move the file there afterwards, then
`publish_artifact` with that path. Videos stream with range support; keep
them under the server's size cap (the `ARTIFACT_TOO_LARGE` message tells you
the number) — a short clip or a lower resolution beats a failed publish.

## A running dev server

When a component needs a framework or a build (an existing app's component
library, a Storybook), start the dev server **from the worktree**, confirm it
answers on `http://localhost:<port>/…`, and publish that URL with
`kind: "url"`. Publish a self-contained HTML snapshot as well when you can; the
URL card dies with the server.

## Common Mistakes

| Mistake | Correction |
|---|---|
| Editing a page and moving on | Re-publish; the person still sees the old revision. |
| `<script src="https://cdn…">` or a Google Font link | Inline it or drop it; the sandbox blocks the network. |
| Saving a generated image in `~/.genmedia/gallery` and publishing that path | Download into `design/media/` first. |
| Pasting the component's HTML into the terminal | One line pointing at the Design tab. |
| Publishing `http://192.168.x.x:3000` | Only loopback URLs are accepted. |
| `git commit` after each change | Only when the person asks. |
