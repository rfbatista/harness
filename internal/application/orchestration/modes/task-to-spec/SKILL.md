---
name: task-to-spec
description: Use in a harness architect session once grilling is done and the affected applications and contracts are known — turn what was settled into one Spec task document per application (problem, solution, user stories, boundary-level decisions, testing seams, scope), ready for a delegate in implement mode. Synthesis only, no interview. Adapted from mattpocock/skills to-spec for task documents.
---

# To Spec

Step two of the architect's loop. Grilling (`task-grill-with-docs`) is over and the
person confirmed the design; you know the affected applications and have written the
contracts between them. Now write **one Spec task document per application**.

**Do not interview the person again.** Synthesise what was already settled in this
session, the Glossary and ADR project documents, and the contracts. The one thing you
check with them is the test seams (step 2).

## Process

1. **Ground it.** For each application, confirm in its repository where the change
   lands — read code only to find the boundary, then stop. Use the Glossary's terms
   throughout and respect the ADRs in the area.
2. **Agree the seams.** For each application, sketch the seams the feature will be
   tested at: prefer existing seams to new ones, the highest seam possible, and as
   few as possible — ideally one. Put them to the person in one message, with a line
   per seam on what it catches and what it misses, and wait for their answer. The
   delegate tests only at these seams; a new one later goes back through you.
3. **Write the spec** with `mcp__task__create_task_document`, one per application,
   from the template below. If a spec for that application already exists on the
   task, change it in place with `mcp__task__edit_task_document` (pass the
   `expected_version` you read) rather than writing a second one.

The delegate breaks the spec into tickets and implements them — so the spec says what
and why at the application's boundary. It names modules and interfaces where a
decision was made about them, but **no file paths and no code**: they go stale before
the work is done. The one exception: a snippet from a prototype that encodes a
decision more precisely than prose (a state machine, a schema, a type shape) — keep
only the decision-rich part and say it came from a prototype.

## `Spec: <application> — <feature>`

A complete HTML page (`create_task_document` refuses Markdown), titled exactly
`Spec: <application> — <feature>` in both the document title and `<title>`. Copy the
template, keep its structure, fill the fields; placeholders are written as `&lt;…&gt;`
so they show as text.

```html
<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Spec: <application> — <feature></title>
<style>
  :root { color-scheme: light dark; }
  body { font: 15px/1.5 system-ui, sans-serif; max-width: 72ch; margin: 2rem auto; padding: 0 1rem; }
  h1, h2 { line-height: 1.25; }
  dt { font-weight: 600; margin-top: 1rem; }
  table { border-collapse: collapse; }
  th, td { border: 1px solid color-mix(in oklab, currentColor 25%, transparent); padding: 0.25rem 0.6rem; text-align: left; }
  code, pre { font-family: ui-monospace, monospace; font-size: 0.92em; }
</style>
</head>
<body>
<h1>&lt;Application&gt; <small>(repository: &lt;repository name&gt;)</small></h1>
<dl>
  <dt>Agent</dt><dd>&lt;agent name from list_agents&gt;</dd>
  <dt>Bounded contexts involved</dt><dd>&lt;names from list_bounded_contexts&gt;</dd>
  <dt>Depends on</dt>
  <dd><ul><li>&lt;Contract or spec documents that must be stable first, by title&gt;</li></ul></dd>
</dl>

<h2>Problem</h2>
<p>&lt;The problem the user faces, from the user's perspective&gt;</p>

<h2>Solution</h2>
<p>&lt;The solution, from the user's perspective, as far as this application delivers it&gt;</p>

<h2>Why this application is affected</h2>
<p>&lt;One paragraph: what the feature requires this application to do differently&gt;</p>

<h2>User stories</h2>
<ol>
  <li>As a &lt;actor&gt;, I want &lt;feature&gt;, so that &lt;benefit&gt;</li>
  <li>&lt;A long list: every aspect of the feature this application carries&gt;</li>
</ol>

<h2>What changes at the boundary</h2>
<ul>
  <li>&lt;API endpoint added/changed, event emitted, schema updated — observable contracts, not internal design&gt;</li>
</ul>

<h2>Implementation decisions</h2>
<ul>
  <li>&lt;Modules built or changed and the interfaces of those modules, where a decision was made&gt;</li>
  <li>&lt;Architectural decisions, schema changes, API contracts, specific interactions, technical clarifications from the person&gt;</li>
</ul>

<h2>Testing decisions</h2>
<ul>
  <li>Seams (agreed with the person): &lt;seam — what it catches, what it misses&gt;</li>
  <li>A good test checks external behaviour at a seam, never implementation details.</li>
  <li>Prior art: &lt;similar tests already in the repository&gt;</li>
</ul>

<h2>Constraints</h2>
<ul>
  <li>&lt;Non-functional requirements: latency, privacy, backward compatibility&gt;</li>
  <li>&lt;Anything the delegate must not break&gt;</li>
</ul>

<h2>Out of scope</h2>
<ul><li>&lt;What this spec deliberately leaves out&gt;</li></ul>

<h2>Further notes</h2>
<p>&lt;Anything else the delegate should know — or "none"&gt;</p>
</body>
</html>
```

User stories cover the whole feature as this application carries it — long and
specific is right. Each `<h2>` is a section, so a delegate reads the one it needs.

When every spec is written, go back to the architect's loop: the overview, then
delegate each spec in `implement` mode.
