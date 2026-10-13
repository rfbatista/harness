---
name: task-grill-with-docs
description: Use in a harness architect session, before any spec is written — interview the person relentlessly about the task until you share one understanding of the design, and record each settled term in the project's Glossary document and each hard-to-reverse decision as an ADR project document while you go. Adapted from mattpocock/skills grill-with-docs (grilling + domain-modeling) for task documents.
---

# Grill With Docs

Step one of the architect's loop. No contract, spec or delegate exists until this
step ends with the person confirming you share an understanding of the design.

You interview the person in this session. Everything you write down is a **project
document** through the `mcp__task__*` tools — never a file in the worktree.

## The design tree

Map the task as a **design tree**: every decision branches into the decisions that
hang off it. Work the tree in **rounds**. The **frontier** is every decision whose
prerequisites are already settled: the questions you can ask *now* without guessing
at answers you have not heard yet. Ask the whole frontier in one round, number each
question and give your recommended answer, then wait for the answers.

```
❓ **Q1** - **<question title>**: <question body, may be several paragraphs, may list choices>

➡️ <your recommended answer>

---

❓ **Q2** - **<question title>**: <question body>

➡️ <your recommended answer>
```

Word each question so "yes" accepts your recommendation. Each round reshapes the
tree: settled decisions push the frontier outward. A question whose answer depends
on another question still open this round belongs to a later round.

**Facts are your job, decisions are theirs.** When a frontier question needs a fact
— what the code does, which repositories exist, what a bounded context owns, what an
earlier task decided — look it up yourself (`mcp__task__list_project_repositories`,
`mcp__task__list_bounded_contexts`, `mcp__task__search_documents`,
`mcp__task__list_project_documents`, reading code, or a sub-agent). Never ask the
person for something you can find. A lookup still running is an unsettled
prerequisite: only the questions downstream of it wait; ask the rest now.

The step is done when the frontier is empty — every branch visited, nothing silently
assumed — and the person confirms you share an understanding. Do not move on to
specs before that confirmation.

## Domain modeling while you grill

Sharpen the project's language as you go.

- **Read the glossary first.** Look for a project document titled
  `Glossary: <project>` with `mcp__task__list_project_documents`, and for
  `ADR:` documents in the area the task touches. Read them in parts
  (`mcp__task__search_documents`, then a `section` or an `offset`/`limit` window,
  never the whole page when it is large).
- **Challenge against the glossary.** When the person uses a term that conflicts with
  it, say so at once: "The glossary defines 'cancellation' as X, but you seem to mean
  Y. Which is it?"
- **Sharpen fuzzy language.** Propose a precise canonical term for a vague or
  overloaded one: "By 'account', do you mean the Customer or the User?"
- **Stress-test with scenarios.** Invent concrete edge cases that force the
  boundaries between concepts to be precise.
- **Cross-reference with code.** When the person states how something works, check
  the code agrees, and surface a contradiction as a question.

## Recording

Write terms and decisions down the moment they settle; do not batch them up.

- **Glossary.** One project document, `Glossary: <project>`, as an HTML page in the
  shape of [GLOSSARY-FORMAT.md](GLOSSARY-FORMAT.md). If none exists, create it when
  the first term settles: `mcp__task__create_task_document`, then
  `mcp__task__move_document_to_project`. Add or change a term in place with
  `mcp__task__edit_project_document`, passing the `expected_version` you read.
  The glossary holds language only — no implementation details, no spec.
- **ADRs.** Offer one only when the decision is hard to reverse, surprising without
  context, **and** the result of a real trade-off. One project document per decision,
  `ADR: NNNN <short title>`, in the shape of [ADR-FORMAT.md](ADR-FORMAT.md), created
  and moved to project level the same way. Number it one past the highest existing
  `ADR:` title.

## When it ends

Summarise what was settled in one `mcp__task__add_task_comment` for the person (the
decisions, and the Glossary and ADR documents touched), then continue with the
architect's loop: affected applications, contracts, and specs with the
`task-to-spec` skill.
