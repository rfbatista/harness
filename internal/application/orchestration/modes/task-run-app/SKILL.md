---
name: task-run-app
description: Use in a harness architect session only when the person asks you to run, set up or seed the task's application for testing — seed a task database in the application's own database engine, create the task service, set its variables from .env.example, start it on the task's branch and hand the person the test credentials. The person invokes it; the architect never runs it on its own.
---

# Run the task's application

The person asked to test the task in a running application. You set it up: a **task
database** seeded with one record set per **scenario**, a **task service** running the
task's branch, and the **test credentials** handed over in a **task comment** and a
**Seed document**. Run the steps in this order. Each names the tool it uses and the
check that it worked. Stop at the first failed check and use the Refusals table.

Do not run this skill unless the person asked. Never ask the architect session or a
delegate to run it.

Reference files, in this folder, read only when a step names them:

- `ENGINES.md` — how to detect the engine, and the seed command for each.
- `K3S-SHAPES.md` — names and shapes of the k3s resources you create.
- `SEED-FORMATS.md` — the Seed document and the task comment.

Read large files in parts: `grep -n` for the heading, then `sed -n` that range, never the whole
file. Change a file with Edit, not by rewriting it. A write refused with
`BASE_VERSION_STALE` means the file changed: read it again and redo the change. Ask the
person through `ask_person`, with your recommended answer, and end your turn there; the
answer arrives as your next turn. Never ask in plain text.

## Rules that always hold

- Never write into the repository or any worktree (the SQLite file is the one exception, step 5).
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

If there is no `k3s` server, no `services` server or no task, stop and say which is missing (Refusals). Overwrite, never add: a rerun overwrites its own files.

### 1. Find the task's branch

1. Call `mcp__task__list_task_sessions`. Collect the branches of the task's delegate sessions.
2. One branch: use it. Several: ask which, recommending the one most recently reported
   done. No branch: stop (Refusals).
3. Run `git rev-parse <branch>` in the repository. Check it resolves; note the tip. If it
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
2. Found: use it. Change nothing in it. Check its pod is `Running`.
3. Not found: create `task-<task short id>` as a new namespace with a Secret, a
   Deployment (or StatefulSet) and a Service, with the shapes in `K3S-SHAPES.md`. Check
   the pod becomes `Running` and `pods_exec` can run the engine's client.

### 4. Expose the engine and create the task database

1. Create the NodePort Service `<engine>-nodeport-task-<task short id>` in the engine's
   namespace, selecting the engine pod's labels, beside the existing Service
   (`K3S-SHAPES.md`). Never edit the existing Service. A rerun finds it and reuses it.
2. Read the node's InternalIP from the k3s node tools. The host and port the application
   reaches are that address and the NodePort.
3. With `pods_exec` and the engine's own client, drop and recreate the task database and
   role `task_<task short id>` (letters, digits and `_` only), the role its owner, with a
   fresh password. Check you can connect as that role.

### 5. Derive the scenarios and write the seed

1. Read the task's Spec documents (user stories) and Ticket documents (acceptance
   criteria) in `task/docs/`, in parts. Each is one **scenario**.
2. Run the repository's migration command first, if it documents one, so the schema is
   the branch's tip. Find the command in the repository's docs or Makefile. Do not guess it.
3. For each scenario write the records it needs. Create the application user through the
   application's own login: an account, a token or a claim token. If the application has
   no login, record that.
4. Apply the seed with `pods_exec` (postgres, mysql) or the `sqlite3` client or the
   migration command (SQLite: a file under a task-named path in the service worktree,
   `ENGINES.md`). Check by counting rows per scenario.

### 6. Derive the variables

1. Take every key of the repository's `.env.example` with its default.
2. Override only what the task needs: the database connection to the task database, the
   port, the application user's credentials.
3. A value the task needs that is not in `.env.example` and whose right value you cannot
   tell: ask. Never invent it.
4. Check the run command: does it read `.env` from disk itself (a Makefile with
   `-include .env`, a dotenv library)? The repository's saved env files are written into
   the service worktree before each run and could override the service's variables. If
   so, say so in the comment and prefer a run command that takes the environment over the
   file. Cannot find one: ask.

### 7. Create or update the task service

1. Name it `<repository>-<task short id>`. Call `mcp__services__list_services`.
2. Find the repository's single documented run command (README, Makefile, docs). None or
   several: ask. Find the application's default port. Another service of the project uses
   it: ask for a free one.
3. Not found: call `mcp__services__create_service` with `name`, `kind: "command"`,
   `repository`, `export_repository_env: false`, `env` (the variables of step 6),
   `command: { line, ports }` and a `reason`. This blocks until the person answers.
4. Found: call `mcp__services__update_service` with `changes.env.set` and `unset` for
   what differs, and a `reason`. Nothing to change: do not call it.
5. Check the returned ServiceView has `kind: "command"` and the right `env_names`.

### 8. Start it on the branch

1. Call `mcp__services__start_service` with `worktree: "branch:<name>"`, or
   `"branch:origin/<name>"` for a branch only on origin, and a `reason`. No `env`:
   run overrides are never used, so Start from the page repeats the same run.
2. Already running (`already_running: true`) or the variables changed: call
   `mcp__services__restart_service` with the same `worktree` and a `reason`, so it runs
   the branch's new tip.
3. Check `run.where` reads `branch <name> @ <sha>` with the tip from step 1 and
   `status` is `running`.

### 9. Verify

1. Call `mcp__services__read_service_logs` with `tail_lines: 100`. Look for the listening
   line and for errors. Follow with `offset` if it is still starting.
2. Request the service's URL (its port, on the harness host) and check the application
   answers. Try the login you created.
3. Failed: put the log's last decisive lines in the comment, and say what did not work.

### 10. Write the Seed document and the comment

Follow `SEED-FORMATS.md`.

1. Write `task/docs/` a document titled exactly `Seed: <task title>`: each scenario, the
   records seeded for it, how to reach it in the application. Overwrite it on a rerun.
   Do not add another file.
2. Write the **task comment** in `task/comments/` as `run-app.md`, with the **test
   credentials** in clear, what you read to find the engine, what you did and what you
   could not do. Overwrite it on a rerun.
3. Tell the person in one line where to find both.

## Rerun

When the task database or the task service already exists:

1. Reuse the engine and the NodePort Service. Drop and recreate the task database and
   role, and seed again from the current Spec and Ticket documents.
2. `update_service` only if the variables differ. `restart_service` on the branch so it
   runs the new tip.
3. Overwrite the comment and the Seed document.

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
| Run command reads `.env` from disk and it cannot be avoided | Say so in the comment and ask. |
| `SERVICE_REQUEST_DENIED` | Do not retry. Read the note; change the request or ask. |
| `SERVICE_REQUEST_EXPIRED` | Say it expired and ask whether to file it again. |
| `SERVICE_NAME_TAKEN` | `list_services`; the name is in use by a service not yours: ask. |
| `WORKTREE_NOT_AVAILABLE`, `SANDBOX_UNAVAILABLE` | Stop. Quote the message in the comment. |
| Service `failed`, or the application does not answer | Put the log lines in the comment, say what failed. Do not stop the engine or drop anything. |
| An existing k3s resource would be replaced or deleted | Do not. Create a task-named resource beside it. |
