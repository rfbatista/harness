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
- **Reuse the project's assets.** Check `mcp__task__list_project_artifacts`
  before you draw a logo, palette or component: build on what the project
  already keeps. Once an asset of yours is something other tasks will reuse,
  move it to project level with `mcp__task__move_artifact_to_project`.
- **Commit nothing unless asked.** The worktree is the working surface.
- Titles and notes are shown as plain text; keep them short and human.
- **Notes you leave on the task are HTML pages.** `create_task_document` takes
  a complete HTML document (doctype, head with title and charset, body) and
  refuses Markdown. A short self-contained HTML page is enough.

## Tools

| Tool | Use it to |
|---|---|
| `mcp__task__publish_artifact` | Show a worktree file (`path`) or a loopback dev server (`url`) in the Design tab. `title` required; `note` says what changed; `kind` only if inference would be wrong (`page`, `image`, `video`, `url`, `file`). |
| `mcp__task__list_task_artifacts` | See what this and earlier sessions already published before making more; each item says its `scope`. |
| `mcp__task__list_project_artifacts` | The design assets the project keeps, from any task: logos, palettes, components, reference screens to build on. |
| `mcp__task__move_artifact_to_project` | Keep one of this task's artifacts at project level, for other tasks to reuse. |
| `mcp__task__move_artifact_to_task` | Move a project asset of this task back to task scope (before unpublishing it). |
| `mcp__task__unpublish_artifact` | Take one of your own cards down (the file stays). A project asset is moved back to the task first. |
| `mcp__task__get_task` / `read_task_document` | The brief, and any design notes or specs earlier sessions left (documents are HTML pages; older ones may be Markdown). |

`publish_artifact` returns `{artifact_id, revision, kind, view_url}`. Its
errors name the rule you broke: `ARTIFACT_PATH_OUTSIDE_WORKTREE` (move the
file under the worktree), `ARTIFACT_NOT_FOUND` (path wrong or a directory),
`ARTIFACT_URL_NOT_LOCAL` (only `http://localhost:…` / `http://127.0.0.1:…`),
`ARTIFACT_TOO_LARGE` (the message states the cap; shorten or compress),
`ARTIFACT_KIND_MISMATCH` (omit `kind`). The scope tools add
`ARTIFACT_NOT_ON_TASK` (only artifacts published on this task move),
`ARTIFACT_NOT_PROMOTABLE` (a url card cannot move; publish an HTML snapshot
and move that) and `ARTIFACT_IN_PROJECT` (unpublish refuses a project asset;
move it back with `move_artifact_to_task` first).

## Components and screens

1. Read the task, `list_project_artifacts` and `list_task_artifacts`.
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

## Project assets

A logo, a palette, a component or a reference screen that other tasks will
reuse belongs at project level. `move_artifact_to_project` keeps it there:
the harness takes its own copy, so it outlives this session and its worktree,
and the person sees it in the project's design-assets library. Re-publishing
the same path from this session refreshes that copy while the session lives.
Leave one-off drafts and explorations as task artifacts.

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
| Drawing a new logo or palette when the project has one | `list_project_artifacts` first, and build on it. |
| Unpublishing a project asset | Move it back with `move_artifact_to_task` first, or leave it for the person. |
