# Glossary document format

One file per project, `project/docs/glossary.md` in your virtual shell, titled
`Glossary: <project>` in its first heading.

## Template

```markdown
# Glossary: <project>

<One or two sentences: what this project is and why it exists.>

## <Bounded context or cluster of terms>

**Order**
: <One or two sentences: what the term IS.>
  _Avoid: Purchase, transaction_

**Invoice**
: A request for payment sent to a customer after delivery.
  _Avoid: Bill, payment request_

## Relationships

- <Context A> → <Context B>: <how they relate, e.g. which event one emits and the other consumes>
```

Each term starts its own bolded line, so `grep -n '^\*\*Order\*\*'` finds it and a
single term can be read with `sed -n` or changed with Edit without the rest of the
file.

## Rules

- **Be opinionated.** When several words name one concept, pick the best and list the
  others under *Avoid*.
- **Keep definitions tight.** One or two sentences. Define what it IS, not what it does.
- **Only project-specific terms.** General programming concepts (timeouts, error types,
  utility patterns) stay out even when the project uses them heavily.
- **Group by bounded context** (`list_bounded_contexts` names them) when the project has
  several; a flat list is fine when it has one. Add **Relationships** only when there
  are several contexts.
