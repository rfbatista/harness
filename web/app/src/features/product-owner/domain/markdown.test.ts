import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { isSafeHref, isSiteHref, parseMarkdown } from "./markdown";

describe("an answer's Markdown", () => {
  test("is paragraphs, small headings and real lists", () => {
    const blocks = parseMarkdown("## Done\n\nFirst line\nsecond line\n\n- one\n- two\n\n1. a\n2. b");
    assert.deepEqual(blocks, [
      { kind: "heading", inline: [{ kind: "text", text: "Done" }] },
      { kind: "paragraph", inline: [{ kind: "text", text: "First line second line" }] },
      { kind: "list", ordered: false, items: [[{ kind: "text", text: "one" }], [{ kind: "text", text: "two" }]] },
      { kind: "list", ordered: true, items: [[{ kind: "text", text: "a" }], [{ kind: "text", text: "b" }]] },
    ]);
  });

  test("has bold, italic, code and links inside a line", () => {
    const [p] = parseMarkdown("A **bold** and *soft* word, `code`, and [docs](https://example.com/x).");
    assert.deepEqual(p, {
      kind: "paragraph",
      inline: [
        { kind: "text", text: "A " },
        { kind: "strong", inline: [{ kind: "text", text: "bold" }] },
        { kind: "text", text: " and " },
        { kind: "emphasis", inline: [{ kind: "text", text: "soft" }] },
        { kind: "text", text: " word, " },
        { kind: "code", text: "code" },
        { kind: "text", text: ", and " },
        { kind: "link", href: "https://example.com/x", inline: [{ kind: "text", text: "docs" }] },
        { kind: "text", text: "." },
      ],
    });
  });

  test("a link to a task page is a task reference", () => {
    const [p] = parseMarkdown("See [Project product owner](/projects/p1/tasks/t9).");
    assert.deepEqual(p?.kind === "paragraph" ? p.inline[1] : null, { kind: "task", taskId: "t9", href: "/projects/p1/tasks/t9", title: "Project product owner" });
  });

  test("keeps a fenced block as it is", () => {
    assert.deepEqual(parseMarkdown("```\na < b\n  c\n```"), [{ kind: "fence", text: "a < b\n  c" }]);
  });

  test("shows raw HTML as text, never as markup", () => {
    const [p] = parseMarkdown("<script>alert(1)</script> <b>hi</b>");
    assert.deepEqual(p, { kind: "paragraph", inline: [{ kind: "text", text: "<script>alert(1)</script> <b>hi</b>" }] });
  });

  test("links only to http(s) and to pages of this site; any other target is plain text", () => {
    for (const bad of ["javascript:alert(1)", "data:text/html,x", "vbscript:msgbox(1)", "//evil.example/x", "/\\evil.com", "/\\/evil.com", "//evil.com", "ftp://x"]) {
      const [p] = parseMarkdown(`[click](${bad})`);
      assert.deepEqual(p, { kind: "paragraph", inline: [{ kind: "text", text: "click" }] }, bad);
    }
    const [p] = parseMarkdown("[board](/projects/p1)");
    assert.deepEqual(p?.kind === "paragraph" ? p.inline[0] : null, { kind: "link", href: "/projects/p1", inline: [{ kind: "text", text: "board" }] });
  });

  test("a path is a site link only when the browser keeps it on this origin", () => {
    for (const bad of ["/\\evil.com", "/\\/evil.com", "//evil.com", "javascript:alert(1)", "data:text/html,x", "vbscript:msgbox(1)", "projects/p1"]) {
      assert.equal(isSafeHref(bad), false, bad);
      assert.equal(isSiteHref(bad), false, bad);
    }
    // a percent-encoded backslash is a path character; the browser does not decode it into a host
    for (const good of ["/%5Cevil.com", "/projects/p1", "/projects/p1/tasks/t9?x=1#y"]) {
      assert.equal(isSafeHref(good), true, good);
      assert.equal(isSiteHref(good), true, good);
    }
    assert.equal(isSafeHref("https://example.com/x"), true);
    assert.equal(isSiteHref("https://example.com/x"), false);
  });

  test("an unfinished marker stays as typed, as a streaming answer may end in one", () => {
    const [p] = parseMarkdown("Almost **bold");
    assert.deepEqual(p, { kind: "paragraph", inline: [{ kind: "text", text: "Almost **bold" }] });
  });

  test("an empty text is no blocks", () => {
    assert.deepEqual(parseMarkdown(""), []);
    assert.deepEqual(parseMarkdown("\n\n"), []);
  });
});
