---
name: task-run-app
description: Use in a harness operate-mode session only when the architect's prompt carries the person's request to run, set up or seed the task's application for testing — seed a task database in the application's own database engine, create the task service, set its variables from .env.example, start it on the task's branch and write the test credentials into the Seed document. Outside operate mode the skill only spawns: a session in any other mode, an architect included, never runs a step and spawns an Operator with that request.
---

# Run the task's application

The person asked to test the task in a running application. You set it up: a **task
database** seeded with one record set per **scenario**, a **task service** running the
task's branch, and the **test credentials** written into the **Seed document**. Run the steps in this order. Each names the tool it uses and the
check that it worked. Stop at the first failed check and use the Refusals table.

## Before you start

Check your session's mode first. If you are not in operate mode, whatever your role
(architect, implementer or peer), you are not the Operator: run no step of this skill, run
no service, touch no k3s resource and seed no database. Instead:

1. Call `mcp__task__start_task_session` with `mode: "operate"` and the default branch as
   `base_branch`. Put the person's request in its prompt.
2. Wait for the Operator's done report, or answer its questions and blocked reports.
3. Point the person at the Seed document it attached.

In operate mode, run this skill only when your prompt carries the person's request, which
the session that spawned you passes on. Then the steps below are yours.

Reference files, in this folder, read only when a step names them:

- `ENGINES.md` — how to detect the engine, and the seed command for each.
- `K3S-SHAPES.md` — names and shapes of the k3s resources you create.
- `SEED-FORMATS.md` — the Seed document, with the test credentials.

Read large files in parts: `grep -n` for the heading, then `sed -n` that range, never the whole
file. Change a file with Edit, not by rewriting it. A write refused with
`BASE_VERSION_STALE` means the file changed: read it again and redo the change. Ask the
person through `ask_person`, with your recommended answer, and end your turn there; the
answer arrives as your next turn. Never ask in plain text.

## Rules that always hold

- Never write into the repository or any worktree.
- In k3s, never delete, never scale, never replace an existing resource. Add only. Drop
  only the task database and role you created yourself.
- Never guess an engine, branch, command, port or variable value. Ask.
- Every change to a service is a service request. `create_service`, `update_service`,
  `start_service` and `restart_service` each block until the person answers it. Give a
  `reason` every time, say what the request does and why. If the answer is
  `SERVICE_REQUEST_DENIED`, do not retry the same request: read the note, then change
  the request or ask the person.
- k3s changes ask nobody. They are additive and task-named.
- Use the glossary's words: task database, task service, scenario, Seed document, test credentials.

## Steps

### 0. Check what you have

1. Read `task/task.json` for the task id. The **task short id** is its first 8 characters.
2. Call `mcp__services__list_services`. Check it answers.
3. Call a `k3s` MCP tool that lists namespaces. Check it answers.

If there is no `k3s` server, no `services` server or no task, stop and say which is missing (Refusals).

### 1. Find the task's branch

1. Call `mcp__task__list_task_sessions`. Collect the branches of the task's delegate sessions.
2. One branch: use it. Several: ask which, recommending the one most recently reported
   done. No branch: stop (Refusals).
3. Run `git rev-parse <branch>` in your own worktree (branches are shared, so no branch
   action is needed). Check it resolves; note the tip. If it
   exists only on origin, name it `origin/<name>`.

### 2. Detect the engine

1. Read the repository per `ENGINES.md`, in its order: `.env.example`, dependency manifests,
   migrations, compose and deploy files.
2. Check you can name the engine (postgres, mysql or SQLite) and the files that said so.
   Several engines supported and `.env.example` picks none: ask. Cannot tell, or another
   engine: ask how to proceed. Do not go on without an answer.

### 3. Find or create the engine in k3s (skip for SQLite)

1. Use the `k3s` MCP tools for pods and resources. List pods in every namespace and find
   one running the engine, by image and port (`ENGINES.md`).
2. Found one: use it. Change nothing in it. Check its pod is `Running`. Found several
   of the detected kind: ask which, recommending the one in namespace `default`. Never
   pick silently.
3. Not found: create `task-<task short id>` as a new namespace with a Secret, a
   Deployment (or StatefulSet) and a Service, with the shapes in `K3S-SHAPES.md`. Check
   the pod becomes `Running` and `pods_exec` can run the engine's client.

### 4. Expose the engine and create the task database (skip for SQLite)

1. Create the NodePort Service `<engine>-nodeport-task-<task short id>` in the engine's
   namespace, selecting the engine pod's labels, beside the existing Service
   (`K3S-SHAPES.md`). Never edit the existing Service. A rerun finds it and reuses it.
2. Read the node's InternalIP from the k3s node tools. The host and port the application
   reaches are that address and the NodePort.
3. With `pods_exec` and the engine's own client, drop and recreate the task database and
   role `task_<task short id>` (letters, digits and `_` only), the role its owner, with a
   fresh password. Check you can connect as that role.

### 5. Derive the scenarios and the variables

1. Read the task's Spec documents (user stories) and Ticket documents (acceptance
   criteria) in `task/docs/`, in parts. Each is one **scenario**.
2. Take every key of the repository's `.env.example` with its default.
3. Override only what the task needs: the database connection to the task database, the
   port, the application user's credentials. For SQLite the database path variable
   points at `task_<task short id>.db` under the service worktree's data folder
   (`ENGINES.md`).
4. A value the task needs that is not in `.env.example` and whose right value you cannot
   tell: ask. Never invent it.
5. Check the run command: does it read `.env` from disk itself (a Makefile with
   `-include .env`, a dotenv library)? The repository's saved env files are written into
   the service worktree before each run and could override the service's variables. If
   so, say so in the Seed document and prefer a run command that takes the environment over the
   file. Cannot find one: ask.

### 6. Create or update the task service

1. Name it `<repository>-<task short id>`. Call `mcp__services__list_services`.
2. Find the repository's single documented run command (README, Makefile, docs). None or
   several: ask. Find the application's default port. Another service of the project uses
   it: ask for a free one.
3. Not found: call `mcp__services__create_service` with `name`, `kind: "command"`,
   `repository`, `export_repository_env: false`, `env` (the variables of step 5),
   `command: { line, ports }` and a `reason`. This blocks until the person answers.
4. Found: call `mcp__services__update_service` with `changes.env.set` and `unset` for
   what differs, and a `reason`. Nothing to change: do not call it.
5. Check the returned ServiceView has `kind: "command"` and the right `env_names`.

### 7. Start it on the branch

1. Call `mcp__services__start_service` with `worktree: "branch:<name>"`, or
   `"branch:origin/<name>"` for a branch only on origin, and a `reason`. No `env`:
   run overrides are never used, so Start from the page repeats the same run.
2. Already running (`already_running: true`) or the variables changed: call
   `mcp__services__restart_service` with the same `worktree` and a `reason`, so it runs
   the branch's new tip.
3. Check `run.where` reads `branch <name> @ <sha>` with the tip from step 1. A service
   that exits because the schema is missing is expected here: go on to step 8.

### 8. Get the schema from the branch

The schema comes from the branch, never from you. Never hand-write a schema.

1. Call `mcp__services__read_service_logs`. Does the application apply its own
   migrations at boot? Check the log for the migration lines and the `running` status.
2. Yes: the schema came from boot migrations. Check the tables exist with `pods_exec`.
3. No: find the repository's documented migration command (README, Makefile, docs).
   Run it against the task database, with the connection variables of step 5, from the
   task's delegate session worktree. You may read that worktree; a run does not write
   to it. Find the path with `mcp__task__list_task_sessions`. Check the tables exist. If the service had exited, call
   `mcp__services__restart_service` with the same `worktree` and a `reason` so the
   application comes up on the schema.
4. No documented command, or you cannot run it from there: ask.
5. SQLite: the application or the command line applies migrations at start. Go to step 9.
   If the application has no migration path, ask.

### 9. Seed

1. For each scenario write the records it needs, as SQL. Create the application user
   through the application's own login: an account, a token or a claim token. If the
   application has no login, record that.
2. Apply it with `pods_exec` and the engine's client (postgres, mysql), as the task
   role. Check by counting rows per scenario.
3. SQLite: save the SQL as the variable `SEED_SQL` on the task service with
   `update_service`, change the command line so it applies the seed after the migration
   step and before the run command, then `restart_service` (`ENGINES.md`).
4. Only if the application caches and needs a restart to see the seed, call
   `restart_service` with the same `worktree` and a `reason`.

### 10. Verify

1. Call `mcp__services__read_service_logs` with `tail_lines: 100`. Look for the listening
   line and for errors. Follow with `offset` if it is still starting.
2. Request the service's URL (its port, on the harness host) and check the application
   answers. Try the login you created.
3. Failed: put the log's last decisive lines in the Seed document, and say what did not work.

### 11. Write the Seed document and report

Follow `SEED-FORMATS.md`.

1. Write `task/docs/` a document titled exactly `Seed: <task title>`: the test credentials
   in clear, each scenario, the records seeded for it, how to reach it in the application,
   what you read to find the engine, what you did and what you could not do. Say whether the
   schema came from boot migrations or from a command. Overwrite it on a rerun. Write no task comment
   and no other file.
2. Report. With an architect, send `message_architect` kind `status_report`, status `done`,
   with the Seed document attached (its id from `.index.json`) and the service names, where
   each runs and its URLs. With no architect, leave one short pointer comment in
   `task/comments/` that names the Seed document, without the credentials.

## Rerun

When the task database or the task service already exists:

1. Reuse the engine and the NodePort Service. Drop and recreate the task database and
   role, and seed again from the current Spec and Ticket documents.
2. `update_service` only if the variables differ. `restart_service` on the branch so it
   runs the new tip.
3. Overwrite the Seed document and write no comment. Overwrite, never add: a rerun
   overwrites its own file and creates no new file.

## Refusals

| Situation | What to do |
|---|---|
| Session has no `k3s` MCP server, or no `services` MCP server | Stop. Say which is missing and that the session must be started with it. Change nothing. |
| No task (no `task/task.json`) | Stop. Say the skill needs a task. |
| The task has no branch to run | Stop. Say no delegate branch exists yet. |
| Several candidate branches | Ask which, recommend the one most recently reported done. |
| Engine unknown, several engines and `.env.example` picks none, or not postgres, mysql or SQLite | Ask how to proceed. Do not guess. |
| No documented run command, or several | Ask which command. |
| Default port used by another service | Ask for a free port. |
| Variable not in `.env.example`, value unknown | Ask. Never invent it. |
| Run command reads `.env` from disk and it cannot be avoided | Say so in the Seed document and ask. |
| `SERVICE_REQUEST_DENIED` | Do not retry. Read the note; change the request or ask. |
| `SERVICE_REQUEST_EXPIRED` | Say it expired and ask whether to file it again. |
| `SERVICE_NAME_TAKEN` | `list_services`; the name is in use by a service not yours: ask. |
| `WORKTREE_NOT_AVAILABLE`, `SANDBOX_UNAVAILABLE` | Stop. Quote the message in the Seed document. |
| Service `failed`, or the application does not answer | Put the log lines in the Seed document, say what failed. Do not stop the engine or drop anything. |
| An existing k3s resource would be replaced or deleted | Do not. Create a task-named resource beside it. |
