# Glossary document format

One project document per project, titled `Glossary: <project>`. Like every task
document it is a complete HTML page; `create_task_document` refuses Markdown.

## Template

```html
<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Glossary: <project></title>
<style>
  :root { color-scheme: light dark; }
  body { font: 15px/1.5 system-ui, sans-serif; max-width: 72ch; margin: 2rem auto; padding: 0 1rem; }
  h1, h2 { line-height: 1.25; }
  dt { font-weight: 600; margin-top: 1rem; }
  .avoid { font-style: italic; opacity: 0.8; }
</style>
</head>
<body>
<h1>Glossary: &lt;project&gt;</h1>
<p>&lt;One or two sentences: what this project is and why it exists.&gt;</p>

<h2>&lt;Bounded context or cluster of terms&gt;</h2>
<dl>
  <dt>Order</dt>
  <dd>&lt;One or two sentences: what the term IS.&gt;
    <div class="avoid">Avoid: Purchase, transaction</div></dd>
  <dt>Invoice</dt>
  <dd>A request for payment sent to a customer after delivery.
    <div class="avoid">Avoid: Bill, payment request</div></dd>
</dl>

<h2>Relationships</h2>
<ul>
  <li>&lt;Context A&gt; → &lt;Context B&gt;: &lt;how they relate, e.g. which event one emits and the other consumes&gt;</li>
</ul>
</body>
</html>
```

Each `<dt>` is a term, so the read tools' outline lists every term and a single term
can be read or edited without the rest of the page.

## Rules

- **Be opinionated.** When several words name one concept, pick the best and list the
  others under *Avoid*.
- **Keep definitions tight.** One or two sentences. Define what it IS, not what it does.
- **Only project-specific terms.** General programming concepts (timeouts, error types,
  utility patterns) stay out even when the project uses them heavily.
- **Group by bounded context** (`list_bounded_contexts` names them) when the project has
  several; a flat list is fine when it has one. Add **Relationships** only when there
  are several contexts.
