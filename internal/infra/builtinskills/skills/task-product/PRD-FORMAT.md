# PRD format

The PRD is one file on the feature task: `task/docs/prd.md` (or
`tasks/<id>/docs/prd.md` when the session started without a task), titled
`# PRD: <feature>` so other sessions find it with `grep -l '^# PRD:'`. Keep its
structure, fill every section, write in plain short sentences, and edit it in
place with Edit or `sed -i` when something changes.

## Template

```markdown
# PRD: <feature>

- **Status:** draft | confirmed
- **Feature task:** <task id>

## Goal

<What changes for the person or the business when this ships, in two or three
sentences, and the one outcome that tells you it worked. Why now.>

## Users

<Who this is for, by their job or problem. Who it is not for. What they do
today instead.>

## Scope in

- <One capability of the first version per line, as the user sees it>

## Scope out

- <What this will not do, even though someone will ask, one per line, with why>

## Risks

- <An assumption nobody has checked, a dependency, a constraint, a rollout
  concern — one per line, each with what would reduce it>

## Open questions

- <What is still undecided, and who decides — or "none">

## Child tasks

| Order | Task | Id | Brief |
|---|---|---|---|
| 1 | <child task title> | <id> | `tasks/<id>/docs/brief.md` |
```

- **Goal, Users, Scope in, Scope out, Risks** are the four interview topics;
  the PRD is `confirmed` only once the person has confirmed each.
- **Scope in** lists capabilities, not screens or endpoints. Each child task
  covers one or more of these lines; every line is covered by some child.
- **Child tasks** is filled last, after `create_task` returned each id, in the
  order to take them: the ones that unblock others first.
