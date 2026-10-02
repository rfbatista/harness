---
name: agent-delegation
description: Use when a spec, plan, or task needs to be handed off from your session to another agent persona (e.g. software-architect handing a spec to go-developer) so that agent actually starts the work, rather than the handoff being an inert label. Covers finding or launching the target agent's session and getting the task to it.
---

# Agent Delegation

## Overview

Launching a persona with `agent <name>` (backed by `agents/agent_launch.py`) starts a
**separate `claude` process** — its own session, its own context, its own memory. A
coordinator persona (currently `software-architect`) does not share a process with the
personas it hands work to, so "assign a planning agent" cannot be satisfied by writing
`**Agent:** go-developer` in a spec and moving on — nothing reads that label. Delegating
means actually reaching that other session.

This skill is the delegation step itself: given a target agent key and a task (normally a
spec file already written to disk), get the work in front of a real, running session of
that agent, and know how to check back on it. It says nothing about *what* the task is —
callers like `feature-architecture` own that.

## Interface

This is the part that must stay stable no matter which mechanism implements it below —
treat it as the contract callers rely on:

```
delegate(agent_key, spec_path) -> a way to check on progress later
```

- `agent_key` — a key from `agents/agents.json` (e.g. `go-developer`, `backend-developer`).
- `spec_path` — path to a spec/task file already written to disk (see "Where specs live"
  below). Delegation does not invent the task; it hands off one that already exists.
- Return — some handle the caller can use later to check in. Delegation is **not**
  synchronous: never claim the delegated work is done, or fabricate its outcome, before
  that check-in actually confirms it.

Any future implementation of the "Current mechanism" section below must keep this
interface. Callers (`feature-architecture` and any future coordinator skill) call
`delegate(agent_key, spec_path)` and never need to know which mechanism is behind it.

## Where specs live

Write the spec to `.claude/plans/<app-key>-<feature-slug>.md` under the current project
(the `plans_dir` setting in `agents/agents.json`, already the convention `agent --plan`
reads from). Delegation always hands off a path under this convention, not spec text
pasted inline — the receiving session reads the file itself.

## Current mechanism: cross-session messaging

Uses Claude Code's native `ListAgents`/`SendMessage` tools, which reach other local
`claude` sessions on the same machine — no new protocol of this repo's own.

**Predictable naming:** `agent_launch.py` now sets `--name <agent-key>-<project>-<repository>`
on every launch under `~/projetos/<project>/<repository>/...` (the `projects_root`
setting), derived from the same directory walk `scan_levels`/`project_config_dir` already
use. That means the exact session name a `delegate()` call is looking for can be computed
**before** launching anything, from the current project layout — no slug invention needed.

Steps:

1. **Compute the expected name** — `<agent_key>-<project>-<repository>` for the project
   you're delegating from (same project/repo pair your own session's name would use, if it
   was launched under `~/projetos/...`).
2. **`ListAgents`** — check whether a session with that name is already running.
3. **Launch if missing** —
   `agent <agent_key> --bg "Read the spec at <spec_path> and implement it. When you're
   done or blocked, message back."` The `--bg` flag starts it detached and returns
   immediately; `agent_launch.py` sets the predictable `--name` automatically (step 1), so
   the new session matches what you just looked for.
4. **`SendMessage`** the spec path / task summary to that session's name — this is how you
   reach a session that was already running from step 2, and makes the handoff explicit
   even for a freshly-launched one.
5. **Check back**, don't poll tightly or guess: wait for its reply, or re-run `ListAgents`
   after a reasonable interval. Never report the delegated work as complete until its own
   reply says so.

**Known limitation:** naming is `<agent_key>-<project>-<repository>`, so delegating a
*second, unrelated* task to the same agent in the same project while the first is still
running collides with the same session name. Not solved here — if it becomes a real need,
extend the mechanism (e.g. an explicit task suffix), keeping the `delegate()` interface
above unchanged.

**Unconfirmed at time of writing:** exactly how the receiving session addresses a reply
back to the delegating session (it needs the delegator's own session name/identity).
Confirm this empirically on first real use — until then, the delegating session should be
the one to check back via `ListAgents`/a repeated `SendMessage`, rather than assuming the
target will find its way back unprompted.

## Swapping mechanisms later

The cross-session approach above was chosen for now, but is explicitly not the only valid
implementation of `delegate()`. Two documented alternatives, either of which could replace
the "Current mechanism" section without touching any caller:

- **In-session subagents** — ship `.claude/agents/<agent-key>.md` under the coordinator's
  own `dir` (mirroring the existing `django-developer` → `code-reviewer.md`/
  `github-workflow.md` pattern, wired by `_mirror_subagents` in `agent_launch.py`) and
  dispatch via Claude Code's built-in Agent/Task tool. Synchronous, same session, no
  cross-process messaging — but the subagent doesn't get the target persona's full skill
  set the way a real `agent go-developer` launch does.
- **CLI shell-out** — `agent <agent_key> -p "Read the spec at <spec_path> and implement
  it."` run synchronously (e.g. via Bash), capturing its output directly instead of
  messaging a separate long-running session.

Whichever is chosen, keep `delegate(agent_key, spec_path) -> a way to check on progress`
as the contract callers use.

## Example

```
delegate("go-developer", ".claude/plans/backend-payments-export.md")
```

1. Expected name (delegating from `~/projetos/acme/api`): `go-developer-acme-api`.
2. `ListAgents` — not found.
3. `agent go-developer --bg "Read the spec at .claude/plans/backend-payments-export.md
   and implement it. When you're done or blocked, message back."`
4. `SendMessage` to `go-developer-acme-api` confirming the same spec path.
5. Wait for its reply before reporting this application's work as underway/done.
