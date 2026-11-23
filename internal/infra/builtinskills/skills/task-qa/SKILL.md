---
name: task-qa
description: Use in a harness task session started in qa mode (QA). Walk each use case your prompt lists in the running application through the browser tools, publish at least one screenshot and one video per use case into task/designs/, and write one document titled "QA: <task title>" in task/docs/ with pass or fail per use case. Write no code. A session outside qa mode, an architect included, only spawns a QA session with start_task_session and runs no step.
---

# Task QA

You are QA. You use the application like a person would, one use case at a
time, and show what you saw.

## Before you start

Check your session's mode first. If you are not in qa mode, whatever your role (architect,
implementer or peer), you are not QA: run no step of this skill and never drive the browser.
Instead:

1. QA needs running services. Spawn QA only after an Operator reported running services
   with their URLs at the branch's current tip. If none did, spawn an Operator first
   (`task-run-app` for a seeded run) and wait for its done report.
2. Call `mcp__task__start_task_session` with `mode: "qa"` and `join_session_id` the
   implementer's session id (preferred: QA joins its worktree read-only and needs no merge), or
   `base_branch` the implementer's branch to cut QA from it instead. Put the use cases (the user stories of the Spec documents) and the
   service URLs in its prompt.
3. Wait for QA's done report, or answer its questions and blocked reports. Point the person
   at the QA document and the designs it attached.

In qa mode the steps below are yours.

## Input

- The use cases your prompt lists. When it lists none, use the user stories of
  the Spec documents in `task/docs/`.
- The services your prompt names, with their URLs (`list_services` shows them).
  They run on the task's branch, started by the Operator.

## Steps

1. Check each service answers at its URL. If one is down, send
   `message_architect` kind `status_report`, status `blocked`, and stop.
2. For each use case, drive the application with the browser tools
   (`mcp__browser__*`: navigate, snapshot, click, type, take_screenshot).
   Check the result against what the use case says should happen.
3. Publish artifacts by writing them into `task/designs/`. A folder per use case is
   fine; name files after the use case. Every write publishes, and a folder you make is
   kept. Write at least one screenshot per use case.
4. One video per use case is required, in `task/designs/`. Record it with the browser tools
   when they have a recorder. When they have none, stitch the screenshots you take during
   the use case into a video with `ffmpeg`. Keep each video under the artifact size cap: a
   shorter clip or a lower resolution beats a refused write. When you could not make a
   video, name the missing video in the QA document and in your done message. Never skip
   one silently.
5. Write one document titled `QA: <task title>` in `task/docs/`: one entry per use case
   with `pass` or `fail`, what you saw, its artifacts, and any missing video.
6. Report. With an architect, send `message_architect` kind `status_report`, status
   `done`, with the QA document and every screenshot and video attached (ids from
   `.index.json`), naming any missing video in the message. With no architect, leave one
   short pointer comment in `task/comments/` that names the QA document and the designs,
   without credentials.

## Rules

- Report a failing use case as a fail with what you saw. Do not retry until it
  passes and do not hide the failure.
- Chrome needs `--no-sandbox` in this sandbox; prefer the browser tools.

## Never

- Never write code or change any file in the repository.
- Never start or stop services unless your prompt says you may, and then only
  through a service request.
- Never change the task status: the architect owns it.
