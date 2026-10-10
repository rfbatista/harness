import { createHljs } from "../../../highlight.js";
import { assert, file, test } from "../../../shared/testing/test.js";
import { HLJS_LANGUAGES, TOKENS, hljsHighlighter, scan, scopeOf, tokenOf } from "./hljs-highlighter.js";
import { plainHighlighter } from "./plain-highlighter.js";

file("code/infrastructure/highlighter");

const highlighter = hljsHighlighter(createHljs());

/** One sample per server language id the highlighter maps. */
const SAMPLES = {
  go: 'package main\n\n// Run starts.\nfunc Run(ctx context.Context) error {\n\tx := "a" + 1\n\treturn nil\n}\n',
  templ: 'templ Page(v View) {\n\t<p>{ v.Name }</p>\n}\n',
  javascript: "/* a\n b */\nconst re = /a+/g;\nexport function f(x) { return `t${x}`; }\n",
  typescript: "interface A { b: number }\nconst x: A = { b: 1 };\n",
  html: '<div class="a" id="b"><!-- c --></div>\n',
  css: ".a > #b:hover { color: red; margin: 0 1px; }\n",
  scss: "$a: 1px;\n.b { &:hover { margin: $a; } }\n",
  less: "@a: 1px;\n.b { margin: @a; }\n",
  json: '{ "a": [1, true, null] }\n',
  markdown: "# Title\n\n**bold** and *it* and `code` and [l](http://x)\n- item\n",
  yaml: "a: 1\nb:\n  - 'c' # d\n",
  toml: '[a]\nb = "c"\n',
  sql: "SELECT a, COUNT(*) FROM t WHERE b = 'c';\n",
  shell: 'if [ -f x ]; then echo "$HOME"; fi\n',
  python: 'def f(a: int) -> str:\n    """doc"""\n    return f"{a}"\n',
  ruby: "def a(b)\n  :sym\nend\n",
  rust: 'fn main() { let s: &str = "a"; }\n',
  java: "public class A { int b = 1; }\n",
  kotlin: 'fun a(b: Int): String = "c"\n',
  swift: "let a: Int = 1\n",
  c: "#include <stdio.h>\nint main(void) { return 0; }\n",
  cpp: "template<class T> T a(T b) { return b; }\n",
  csharp: "public class A { string b = \"c\"; }\n",
  php: "<?php function a($b) { return 1; }\n",
  lua: "local a = function(b) return 1 end\n",
  protobuf: "message A { string b = 1; }\n",
  graphql: "query A { b(c: 1) { d } }\n",
  dockerfile: "FROM golang:1.25\nRUN go build ./...\n",
  makefile: "all: build\n\tgo build ./...\n",
  diff: "@@ -1 +1 @@\n-a\n+b\n",
};

test("every registered grammar emits only the contract's 17 classes, line by line", () => {
  const allowed = new Set(["", ...TOKENS]);
  for (const [language, text] of Object.entries(SAMPLES)) {
    assert.ok(highlighter.supports(language), `${language} is supported`);
    const lines = highlighter.tokenize(language, text);
    assert.equal(lines.length, text.split("\n").length, `${language}: one entry per line`);
    for (const line of lines) for (const t of line) assert.ok(allowed.has(t.tok), `${language}: "${t.tok}" is not a contract class`);
    assert.equal(lines.map((l) => l.map((t) => t.text).join("")).join("\n"), text, `${language}: the text comes back unchanged`);
  }
  assert.equal(HLJS_LANGUAGES.length, 29, "every grammar web/src/highlight.js registers");
});

test("Go reads as keywords, types, functions, strings, comments", () => {
  const [, , comment, sig, body] = highlighter.tokenize("go", SAMPLES.go);
  assert.deepEqual(comment, [{ text: "// Run starts.", tok: "comment" }]);
  const toks = (line) => Object.fromEntries(line.filter((t) => t.tok).map((t) => [t.text.trim(), t.tok]));
  assert.equal(toks(sig).func, "keyword");
  assert.equal(toks(sig).Run, "function");
  assert.equal(toks(sig).error, "type");
  assert.equal(toks(body)['"a"'], "string");
  assert.equal(toks(body)["1"], "number");
});

test("a block comment spanning lines stays a comment on each line", () => {
  const lines = highlighter.tokenize("javascript", "/* a\n b */\nx");
  assert.deepEqual(lines[0], [{ text: "/* a", tok: "comment" }]);
  assert.deepEqual(lines[1], [{ text: " b */", tok: "comment" }]);
});

test("an unknown language and plain text stay plain", () => {
  assert.equal(highlighter.supports("cobol"), false);
  assert.deepEqual(highlighter.tokenize("cobol", "a\n\nb"), [[{ text: "a", tok: "" }], [], [{ text: "b", tok: "" }]]);
  assert.deepEqual(plainHighlighter().tokenize("go", "x"), [[{ text: "x", tok: "" }]]);
});

test("the scanner: scopes, nesting, inheritance, subst and entities", () => {
  assert.equal(scopeOf("hljs-title function_ invoke__"), "title.function.invoke");
  assert.equal(scopeOf("language-xml"), "");
  assert.equal(tokenOf("title.function.invoke"), "function");
  assert.equal(tokenOf("title.class.inherited"), "type");
  assert.equal(tokenOf("formula"), undefined);
  const html = '<span class="hljs-string">`a${<span class="hljs-subst">x</span>}&lt;&amp;&#x27;\nb`</span><span class="hljs-formula">c</span>';
  assert.deepEqual(scan(html), [
    [{ text: "`a${", tok: "string" }, { text: "x", tok: "" }, { text: "}<&'", tok: "string" }],
    [{ text: "b`", tok: "string" }, { text: "c", tok: "" }],
  ]);
});
