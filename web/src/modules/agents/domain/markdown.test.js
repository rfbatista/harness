import { assert, file, test } from "../../../shared/testing/test.js";
import { renderMarkdown } from "./markdown.js";

file("agents/domain/markdown");

test("headings, paragraphs, lists, emphasis and code render", () => {
  const html = renderMarkdown("# Title\n\nSome **bold**, *soft* and `code`.\n\n- one\n- two\n\n1. first\n2. second\n");
  assert.ok(html.includes("<h1>Title</h1>"));
  assert.ok(html.includes("<p>Some <strong>bold</strong>, <em>soft</em> and <code>code</code>.</p>"));
  assert.ok(html.includes("<ul><li>one</li><li>two</li></ul>"));
  assert.ok(html.includes("<ol><li>first</li><li>second</li></ol>"));
});

test("a fenced block keeps its text verbatim and unformatted", () => {
  const html = renderMarkdown("```go\nfmt.Println(\"**hi**\" < 3)\n```\n");
  assert.equal(html, '<pre><code>fmt.Println(&quot;**hi**&quot; &lt; 3)</code></pre>');
});

test("embedded HTML is shown as text, never as markup", () => {
  const html = renderMarkdown('Hello <script>alert(1)</script>\n\n<img src=x onerror="alert(2)">\n\n# <b>x</b>');
  assert.ok(!html.includes("<script"), html);
  assert.ok(!html.includes("<img"), html);
  assert.ok(!html.includes("<b>"), html);
  assert.ok(html.includes("&lt;script&gt;alert(1)&lt;/script&gt;"));
  const doc = new DOMParser().parseFromString(html, "text/html");
  assert.equal(doc.querySelectorAll("script, img, b").length, 0);
  assert.equal(doc.querySelectorAll("[onerror]").length, 0);
});

test("a link keeps http, https, mailto and relative targets and drops script targets", () => {
  const html = renderMarkdown("[a](https://x.dev/p?q=1&r=2) [b](javascript:alert(1)) [c]( JaVaScRiPt:alert(1)) [d](#top) [e](data:text/html,x)");
  assert.ok(html.includes('<a href="https://x.dev/p?q=1&amp;r=2" rel="noreferrer noopener" target="_blank">a</a>'), html);
  assert.ok(!/href="\s*javascript/i.test(html), html);
  assert.ok(!html.includes('href="data:'), html);
  assert.ok(html.includes('<a href="#top"'), html);
  const text = new DOMParser().parseFromString(html, "text/html").body.textContent;
  assert.ok(/\bb\b/.test(text) && /\bc\b/.test(text) && /\be\b/.test(text),"the text of a refused link stays");
  assert.equal(new DOMParser().parseFromString(html, "text/html").querySelectorAll("a").length, 2);
});

test("an attribute cannot be broken out of through a link or an image", () => {
  const html = renderMarkdown('[x](https://a.dev/" onmouseover="alert(1))');
  const doc = new DOMParser().parseFromString(html, "text/html");
  assert.equal(doc.querySelectorAll("[onmouseover]").length, 0, html);
});

test("a table renders its header and rows", () => {
  const html = renderMarkdown("| a | b |\n|---|---|\n| 1 | 2 |\n");
  assert.ok(html.includes("<th>a</th><th>b</th>") && html.includes("<td>1</td><td>2</td>"), html);
});

test("nothing renders for an empty body", () => {
  assert.equal(renderMarkdown(""), "");
});
