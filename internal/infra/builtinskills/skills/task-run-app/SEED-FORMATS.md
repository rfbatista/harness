# Seed document

The Seed document is for the person: write it in clear, complete sentences. It is the only
file you write, and it is overwritten on a rerun. Never add a second copy and write no task
comment.

## Seed document

A file in `task/docs/`, for example `seed.md`, whose first heading is exactly:

```
# Seed: <task title>
```

Then:

```
Seeded <date> into task database <name> on <host>:<port>, for the task service <service name>, branch <name> @ <sha>.

## <scenario>
- Source: <Spec user story N, or Ticket NN acceptance criterion>
- Records: <what was seeded, with ids or names the person can recognise>
- How to reach it: <the page, route or call, and which login to use>
```

Say whether the schema came from boot migrations or from a command (name it).

One section per scenario, in the order of the Spec and the Tickets. A scenario with nothing
to seed says so and why.

## Test credentials

The last section of the Seed document:

```
## Test credentials
- Service: <service name>, <URL>
- Engine: <host>:<port>, task database <name>, role <role>, password <password>
- Application login: <user and password, or token, or claim token — or "the application has no login">

## What I did
- Engine: <found in namespace X / created namespace task-NN>, read from <files>
- Branch: <name> @ <sha>
- Service: <created / updated / restarted>, command <line>
- Variables: <keys overridden>; the repository's saved .env is not exported

## What I could not do
- <each gap, or "nothing">
```

The password and the login are in clear: they are test values on a home cluster. If the run
command reads `.env` from disk, say so here.

## Pointer comment

Only when the task has no architect: one short file in `task/comments/` that names the Seed
document. It never holds the credentials.
