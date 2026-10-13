---
name: task-to-spec
description: Use in a harness architect session once grilling is done and the affected applications and contracts are known — turn what was settled into one Spec document per application, a file in task/docs of your virtual shell (problem, solution, user stories, boundary-level decisions, testing seams, scope), ready for a delegate in implement mode. Synthesis only, no interview. Adapted from mattpocock/skills to-spec for task documents.
---

# To Spec

Step two of the architect's loop. Grilling (`task-grill-with-docs`) is over and the
person confirmed the design; you know the affected applications and have written the
contracts between them. Now write **one Spec document per application**, a file in
`task/docs/` of your virtual shell (`$HARNESS_SHELL`).

**Do not interview the person again.** Synthesise what was already settled in this
session, the Glossary and ADR files in `project/docs/`, and the contracts. The one
thing you check with them is the test seams (step 2).

## Process

1. **Ground it.** For each application, confirm in its repository where the change
   lands — read code only to find the boundary, then stop. Use the Glossary's terms
   throughout and respect the ADRs in the area.
2. **Agree the seams.** For each application, sketch the seams the feature will be
   tested at: prefer existing seams to new ones, the highest seam possible, and as
   few as possible — ideally one. Put them to the person in one message, with a line
   per seam on what it catches and what it misses, and wait for their answer. The
   delegate tests only at these seams; a new one later goes back through you.
3. **Write the spec** with Write, one file per application from the template below,
   e.g. `task/docs/spec-backend-csv-export.md`. If a spec for that application
   already exists (`grep -l '^# Spec: backend' task/docs/*`), change it in place with
   Edit rather than writing a second one; on `BASE_VERSION_STALE`, read it again and
   redo the edit.

The delegate breaks the spec into tickets and implements them — so the spec says what
and why at the application's boundary. It names modules and interfaces where a
decision was made about them, but **no file paths and no code**: they go stale before
the work is done. The one exception: a snippet from a prototype that encodes a
decision more precisely than prose (a state machine, a schema, a type shape) — keep
only the decision-rich part and say it came from a prototype.

## `Spec: <application> — <feature>`

Its first heading is its title, exactly `Spec: <application> — <feature>`. Copy the
template, keep its structure, fill the fields.

```markdown
# Spec: <application> — <feature>

- **Repository:** <repository name>
- **Agent:** <agent name from list_agents>
- **Bounded contexts involved:** <names from list_bounded_contexts>
- **Depends on:** <contract or spec documents that must be stable first, by title>

## Problem

<The problem the user faces, from the user's perspective>

## Solution

<The solution, from the user's perspective, as far as this application delivers it>

## Why this application is affected

<One paragraph: what the feature requires this application to do differently>

## User stories

1. As a <actor>, I want <feature>, so that <benefit>
2. <A long list: every aspect of the feature this application carries>

## What changes at the boundary

- <API endpoint added/changed, event emitted, schema updated — observable contracts, not internal design>

## Implementation decisions

- <Modules built or changed and the interfaces of those modules, where a decision was made>
- <Architectural decisions, schema changes, API contracts, specific interactions, technical clarifications from the person>

## Testing decisions

- Seams (agreed with the person): <seam — what it catches, what it misses>
- A good test checks external behaviour at a seam, never implementation details.
- Prior art: <similar tests already in the repository>

## Constraints

- <Non-functional requirements: latency, privacy, backward compatibility>
- <Anything the delegate must not break>

## Out of scope

- <What this spec deliberately leaves out>

## Further notes

<Anything else the delegate should know — or "none">
```

User stories cover the whole feature as this application carries it — long and
specific is right. Each `##` heading is a section, so a delegate finds the one it
needs with `grep -n '^## '` and reads only that.

When every spec is written, go back to the architect's loop: the overview, then
delegate each spec in `implement` mode.
