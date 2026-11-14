# Task brief format

A brief is one file on a child task: `tasks/<id>/docs/brief.md`, titled
`# Brief: <task>` so other sessions find it with `grep -l '^# Brief:'`. It is
what the child's implementer or architect reads first: what to build, when it
is done, which parts of the system it touches, and what it leaves alone. It
says nothing about how to code it.

## Template

```markdown
# Brief: <task title>

- **Feature:** <PRD title> (task <feature task id>)
- **Order:** <n of N>; after <child task titles it depends on, or "none">

## What to build

<The piece of work in a paragraph, as the user experiences it: what they can
do once it ships that they cannot do today. Name the capabilities from the
PRD's Scope in that this task covers.>

## Acceptance criteria

- <One observable behaviour per line, checkable by a person using the product:
  "Given …, when …, then …" or a plain sentence>

## Boundaries touched

- <Each application or bounded context this task changes, one per line, with
  what it exposes or consumes there — the boundary, not the code behind it>

## Out of scope

- <What a reader might expect this task to include and it does not, one per
  line, with where it lives instead (another child, a later version, never)>
```

- **Acceptance criteria** are the done test: if every line holds, the task is
  done. Keep them observable from outside the code.
- **Boundaries touched** names applications (`list_project_repositories`) and
  bounded contexts (`list_bounded_contexts`) by the project's names; it is
  where an architect starts, so stay at the boundary.
- A brief is edited in place with Edit or `sed -i` when the scope changes;
  `BASE_VERSION_STALE` means someone changed it first — read it again.
