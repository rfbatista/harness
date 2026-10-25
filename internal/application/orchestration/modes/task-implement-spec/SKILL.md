---
name: task-implement-spec
description: Use in a harness task session started in implement mode, as a delegate of the task's architect — break the Spec document you were handed into tracer-bullet tickets (files in task/docs of your virtual shell, with blocking edges), have the architect review them, then implement the ticket frontier test first on your branch, review the result against the spec, and report back. Adapted from mattpocock/skills to-tickets and implement-spec for task documents.
---

# Implement Spec

You are a delegate. The architect wrote a `Spec: <application> — <feature>` task
document for your repository and started you to implement it. Your prompt names it.
It is a file in `task/docs/` of your virtual shell (`$HARNESS_SHELL`), beside the
contracts; the project's Glossary and ADRs are in `project/docs/`. Read them with
Read, `grep` and `sed -n`; write with Write, Edit or `sed -i` — each change is stored
as you make it. `.index.json` gives each file's document `id`.

The goal: the whole spec implemented and committed on **your session's branch** —
your integration branch — with every ticket marked done, reviewed against the spec,
and handed back to the architect.

## Hard rules

- **No code before the tickets are approved.** The architect's reply to your ticket
  review request must carry `verdict: approved`.
- **Tickets are files in `task/docs/` of your virtual shell.** Nothing under
  `.scratch/` or `docs/` in the repository; the worktree holds only the code change.
- **Never change a contract you consume.** If a Contract document is wrong or
  missing something, say so with `mcp__task__message_architect` kind question and
  wait.
- **Never move the task status.** The architect owns it; `update_task_status` is
  refused for you.
- **Test only at the spec's agreed seams** (its Testing decisions). A new seam is a
  question for the architect, not a decision you make.
- **Read documents in parts.** `grep -n` for the heading you need, then `sed -n`
  that range (or Read with offset and limit), never the whole file when it is large.
  Change a ticket's status line with Edit or `sed -i`, not by rewriting it. A write
  refused with `BASE_VERSION_STALE` means someone changed the file: read it again
  and redo the change; any other refusal code says the path is not yours to write.

## 1. Read the spec

Read the Spec document and every Contract it depends on (`ls task/docs`,
`grep -l '^# Contract' task/docs/* project/docs/*`). Read the project's `Glossary:`
and the `ADR:` documents in your area in `project/docs/` (`grep -rn` for the terms
you need), and use the glossary's terms in ticket titles, test names and code. Explore the repository as
far as you need to slice the work — look for a prefactor that makes the change easy
("make the change easy, then make the easy change").

Read the repository's architecture record too, `project/architecture/<repo>.yaml` in your
virtual shell, before you write tickets. Keep each ticket inside the layers your agent may
change. Ask the architect (`mcp__task__message_architect`, kind `question`) before a ticket
needs a layer you may not change, or when a rule blocks work the spec requires.

## 2. Write the tickets

Break the spec into **tracer-bullet tickets**:

- Each cuts a narrow but complete path through every layer it needs (schema, API,
  UI, tests) — vertical, never one layer at a time.
- Each is demoable or verifiable on its own, and fits one fresh context window.
- Prefactoring comes first, as its own ticket.
- Each declares its **blocking edges**: the tickets that must be done before it can
  start. One with none can start at once.

**Wide refactors are the exception.** One mechanical change whose blast radius fans
across the codebase (renaming a column, retyping a shared symbol) is sequenced as
expand–contract instead: an *expand* ticket adds the new form beside the old; one
*migrate* ticket per batch of call sites (per package or directory), each blocked by
the expand; a *contract* ticket deletes the old form, blocked by every migrate batch.

Write one file per ticket in `task/docs/`, numbered from `01` in dependency order
(blockers first), titled `Ticket: <application> — NN <title>` in its first heading,
e.g. `task/docs/ticket-backend-01-export-endpoint.md`. No file paths or code
snippets in them.

```markdown
# Ticket: <application> — NN <title>

- **Spec:** <Spec document title>
- **Blocked by:** <ticket numbers and titles — or "none (can start immediately)">
- **Status:** todo
- **Commit:** <filled in when done>

## What to build

<The end-to-end behaviour this ticket makes work, from the user's perspective — not a list of layers>

## Acceptance criteria

- <criterion, observable at one of the spec's seams>
```

Status moves `todo` → `in_progress` → `done` (or `blocked`).

## 3. Get the tickets reviewed

Send `mcp__task__message_architect` with kind `review_request`, a subject like
`Tickets: <application> — <feature>`, every ticket in `document_ids` (their ids in
`.index.json`), and a body that
lists them as a numbered breakdown — title, blocked by, what it delivers — and calls
out the slicing choices you are unsure of (granularity, edges, merges or splits).

Then stop and wait. The reply arrives as a turn starting with `[task message …]`:

- `verdict: approved` → go to step 4.
- `verdict: changes_requested` → edit the ticket files in place, and ask for review
  again, `in_reply_to` the reply.

## 4. Implement the frontier

Your session's branch is the integration branch. The **frontier** is every ticket
whose blockers are all `done`.

- For each frontier ticket, run an **implementer sub-agent** in its own worktree off
  your branch (the Agent tool with `isolation: "worktree"`), in the background when
  several can run at once. Communicate by pointer — the ticket's and the spec's
  paths in your virtual shell, earlier commits — not by pasting them. Each implementer:
  - confirms its worktree is based on your branch's tip, and resets onto it if not;
  - builds the ticket with the `task-tdd` skill, at the spec's seams;
  - merges your branch's tip into its own before reporting done.
- When an implementer finishes, merge its branch into yours, run the repository's
  checks, mark the ticket `done` with its commit, and send a `status_report`
  (status `working`) naming the ticket.
- A finished ticket can widen the frontier: start implementers for the newly
  unblocked tickets.
- One ticket on the frontier at a time is fine to build yourself, in your own
  worktree, with `task-tdd`.
- Blocked — a failing check you cannot fix, a contract question, a missing seam →
  mark the ticket `blocked` and send a `status_report` with status `blocked` saying
  what you need.

For context-heavy exploration the tickets need, a sub-agent can do it once and leave
notes outside the repository for the implementers to read.

## 5. Review against the spec

When every ticket is `done`, run the `task-code-review` skill on your branch against
the base branch it was cut from, with the Spec document as the spec. Fix what it
raises in one more implementer pass, then run the repository's full checks.

## 6. Hand back

- Commit everything on your branch. Do not open a pull request unless the architect
  asks for one.
- Send `mcp__task__message_architect` kind `status_report`, status `done`, naming
  the branch, the tickets, and the review's one-line summary.
- Then kind `review_request` with the Spec and ticket documents attached, asking the
  architect to review the branch.
- Remove the implementer worktrees you created.

A `changes_requested` reply after that is new work: add or reopen tickets, and go
round steps 4–6 again.
