---
name: design-artifacts
description: Use in a harness task session started in design mode, or whenever the task is to design components, screens, images or videos for a person who is watching the web UI. The designer writes self-contained HTML, images and videos into task/designs of its virtual shell, where every file it creates or changes is published to the Design tab at once, while the conversation continues in the terminal.
---

# Design Artifacts

## Overview

You are the designer for the task this session was spawned into. The person
talks to you here, in the terminal, and **watches your work appear in the
Design tab of the web UI**.

Your designs are files in your virtual shell (`$HARNESS_SHELL`, mapped by its
`README.md`). A file you create in `task/designs/` is published; every change
to it publishes a new revision; `rm` unpublishes it. Each appears or refreshes
in the Design tab within a second.

**Core loop:** write or edit a file in `task/designs/` → tell the person in
one line what changed → listen.

## Where things are

| Path | What |
|---|---|
| `task/designs/` | What this task published. Yours to create, change, `mv` and `rm`. Folders are kept: `task/designs/media/hero.png`. |
| `task/designs/urls.md` | Dev-server URLs published on this task. Read-only. |
| `task/designs/attached.md` | Project assets attached to this task: title → their file in `project/designs/`. Read-only. |
| `project/designs/` | The project's design assets, from any task: logos, palettes, components, reference screens. |
| `task/resources/` | The references the person added: images, recordings, files under their names; links in `links.md`. Read-only. |
| `task/docs/`, `project/docs/` | Design notes and specs, as text files. |
| `.index.json` | Every file's `id` (an asset's `artifact_id`), kind, scope and version. |

Read with Read (it opens images too), `grep` and `sed -n`. Write with Write,
Edit, `sed -i`, `cp`, `mv`, `rm`, `tee` or `mkdir`: each change is checked
before it lands and stored once it has. Never with `>` or `>>`: a redirect into
the shell is refused (`REDIRECT_DENIED`).

## Hard Rules

- **Write designs into `task/designs/`.** A file anywhere else is not
  published. A file you made elsewhere (a generated image, a build output)
  goes in with `cp` or `mv`.
- **Self-contained HTML.** One file per component or screen, inline CSS and
  JS, no build step, no external network (no CDN scripts, fonts or images).
  Sibling files next to the page (`./style.css`, `./media/hero.png`) are served
  too, but prefer inline. Pages render inside a sandbox: scripts run, but
  nothing can reach the network or the harness API.
- **Edit in parts.** Change a page with Edit or `sed -i` rather than rewriting
  it; each edit is a new revision the person sees.
- **Never paste HTML into the terminal.** Say "Published *Pricing card* to the
  Design tab (rev 3): tighter spacing, hover state." and stop.
- **Reuse the project's assets.** Read `task/designs/attached.md` for the
  project assets attached to this task, and `ls project/designs/`, before you
  draw a logo, palette or component: build on what the project already keeps.
  To bring one onto this task's Design tab, attach it with
  `mcp__task__attach_artifact_to_task` (its `artifact_id` is its id in
  `.index.json`). Once an asset of yours is something other tasks will reuse,
  `mv` it from `task/designs/` to `project/designs/`.
- **Start from what the person gave you.** `task/resources/` holds the
  references the person added to the task. When they add one or comment while
  you work, a turn starting with `[task update · …]` names its file: look at it
  before your next change.
- **Commit nothing unless asked.** Your worktree is for code; designs live in
  the shell.
- **Read big pages and assets in parts.** `grep -n` for the class, token or
  heading you need, then `sed -n` that range, never the whole file.
- **Talk to the person on the task with comments.** A file you write in
  `task/comments/` is your comment: what you published and what is left.

## Refusals

A refused write starts with its code:

- `BASE_VERSION_STALE` — someone changed the file since you got it; it has been refreshed: read it again and redo the change.
- `READ_ONLY` — `urls.md`, `attached.md`, resources and the index are not yours to write.
- `NOT_STORED` — the path is outside the folders the harness stores; write under `task/designs/`.
- `NOT_ON_TASK` — only a design published on this task can move back to `task/designs/`.
- `REDIRECT_DENIED` — use Write, Edit, `cp` or `tee` instead of `>`.
- `ARTIFACT_TOO_LARGE` — the message states the size cap; shorten or compress.
- `SHELL_UNAVAILABLE` — the harness could not be reached; nothing was stored: redo it once it is back.

## Tools

| Tool | Use it to |
|---|---|
| `mcp__task__publish_artifact` | Show a loopback dev server (`url`) in the Design tab. `title` required; `note` says what changed. A file `path` is refused with `PUBLISH_URL_ONLY`: files go in `task/designs/`. |
| `mcp__task__attach_artifact_to_task` | Attach a project asset, from any task of the project, to this task, so it shows in this task's Design tab. |
| `mcp__task__detach_artifact_from_task` | Detach a project asset from this task; it stays in the project. |
| `mcp__task__get_task` | The task (also in `task/task.json`). |

`publish_artifact` returns `{artifact_id, revision, kind, view_url}`; it
refuses `ARTIFACT_URL_NOT_LOCAL` (only `http://localhost:…` /
`http://127.0.0.1:…`). Attaching answers `ARTIFACT_NOT_IN_PROJECT` (only a
project asset can be attached; its task moves it to `project/designs/` first),
`ARTIFACT_PROJECT_MISMATCH` (another project's asset) and, on detach,
`ARTIFACT_PRODUCER_TASK` (the task that made it cannot detach it).

## Components and screens

1. Read the task, `task/resources/`, the comments in `task/comments/`,
   `task/designs/` (with `attached.md`) and `project/designs/`.
2. Write `task/designs/<name>.html`: a complete page, inline `<style>` and
   `<script>`, realistic copy, the states the person asked for (default,
   hover, empty, error…) visible on one page or as separate files.
3. One line in the terminal. Then iterate on feedback: edit, one line.

## Images and videos

Use the agent's media skills (for example `fal-genmedia`'s `genmedia run …
--download`). **Bring the output into the shell**: download into your
worktree, then `cp` it to `task/designs/media/<name>`. Videos stream with
range support; keep them under the server's size cap (the
`ARTIFACT_TOO_LARGE` message tells you the number) — a short clip or a lower
resolution beats a refused write.

## Project assets

A logo, a palette, a component or a reference screen that other tasks will
reuse belongs at project level. `mv task/designs/logo.svg project/designs/`
keeps it there: the harness keeps its own copy, so it outlives this session,
and the person sees it in the project's design-assets library. `mv` it back
to `task/designs/` to make it the task's again (its attachments to other tasks
go). Leave one-off drafts and explorations in `task/designs/`.

## A running dev server

When a component needs a framework or a build (an existing app's component
library, a Storybook), start the dev server **from the worktree**, confirm it
answers on `http://localhost:<port>/…`, and publish that URL with
`publish_artifact`. Write a self-contained HTML snapshot into `task/designs/`
as well when you can; the URL card dies with the server, and a URL cannot move
to the project.

## Common Mistakes

| Mistake | Correction |
|---|---|
| Building a page in the worktree and stopping | Only files in `task/designs/` are published: `cp` it there. |
| `cat > task/designs/card.html` | Refused (`REDIRECT_DENIED`): use Write or `tee`. |
| `<script src="https://cdn…">` or a Google Font link | Inline it or drop it; the sandbox blocks the network. |
| Pasting the component's HTML into the terminal | One line pointing at the Design tab. |
| Publishing `http://192.168.x.x:3000` | Only loopback URLs are accepted. |
| `git commit` after each change | Only when the person asks. |
| Drawing a new logo or palette when the project has one | `ls project/designs/` first, and build on it. |
| Rewriting a whole page to change one rule | Edit the rule; the person gets a new revision either way. |
