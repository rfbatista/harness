// Highlighter over highlight.js (web/third_party/highlightjs, vendored and
// pinned). hljs returns HTML whose only markup is <span class="hljs-…">,
// </span> and five entities; a string scanner (no DOM, no innerHTML) turns it
// into tokens per line and maps each scope onto the design system's 17 tok-*
// classes. A scope with no class inherits its parent's; subst (code inside a
// string) resets to plain.

/** The server's language ids (internal/domain/code.go) → registered hljs names. */
const LANGUAGES = {
  go: "go", templ: "go", gomod: "go", javascript: "javascript", jsx: "javascript", typescript: "typescript", tsx: "typescript",
  json: "json", markdown: "markdown", html: "xml", xml: "xml", vue: "xml", svelte: "xml", css: "css", scss: "scss", sass: "scss",
  less: "less", yaml: "yaml", toml: "ini", ini: "ini", dotenv: "ini", sql: "sql", shell: "bash", python: "python", ruby: "ruby",
  rust: "rust", java: "java", kotlin: "kotlin", swift: "swift", c: "c", cpp: "cpp", csharp: "csharp", php: "php", lua: "lua",
  protobuf: "protobuf", graphql: "graphql", dockerfile: "dockerfile", makefile: "makefile", diff: "diff",
};

/** The hljs languages main.js registers, by the name above. */
export const HLJS_LANGUAGES = Object.freeze([...new Set(Object.values(LANGUAGES))]);

/** hljs scope → contract token ("" plain; absent: inherit the parent's). */
const SCOPES = {
  keyword: "keyword",
  "variable.language": "keyword",
  built_in: "type",
  type: "type",
  "title.class": "type",
  title: "function",
  "title.function": "function",
  literal: "constant",
  symbol: "constant",
  bullet: "constant",
  "variable.constant": "constant",
  string: "string",
  char: "string",
  code: "string",
  "meta.string": "string",
  number: "number",
  comment: "comment",
  doc: "comment",
  quote: "comment",
  operator: "operator",
  punctuation: "punctuation",
  tag: "tag",
  name: "tag",
  "selector-tag": "tag",
  "template-tag": "tag",
  attr: "attr",
  attribute: "attr",
  "selector-attr": "attr",
  "selector-class": "attr",
  "selector-id": "attr",
  "selector-pseudo": "attr",
  meta: "meta",
  regexp: "regexp",
  section: "heading",
  strong: "heading",
  emphasis: "emphasis",
  link: "link",
  variable: "variable",
  params: "variable",
  property: "variable",
  "template-variable": "variable",
  subst: "",
  addition: "",
  deletion: "",
};

/** The 17 classes the design system styles (contract §2), without their prefix. */
export const TOKENS = Object.freeze([
  "comment", "string", "number", "keyword", "type", "function", "variable", "constant", "operator",
  "punctuation", "tag", "attr", "meta", "regexp", "heading", "emphasis", "link",
]);

/** `class="hljs-title function_ invoke__"` → "title.function.invoke"; "" for language-* wrappers. */
export function scopeOf(classAttr) {
  const parts = classAttr.trim().split(/\s+/);
  if (!parts[0]?.startsWith("hljs-")) return "";
  return [parts[0].slice(5), ...parts.slice(1).map((p) => p.replace(/_+$/, ""))].join(".");
}

/** The token of a scope, trying its parents ("title.function.invoke" → "title.function"); undefined to inherit. */
export function tokenOf(scope) {
  for (let s = scope; s; s = s.includes(".") ? s.slice(0, s.lastIndexOf(".")) : "") {
    if (s in SCOPES) return SCOPES[s];
  }
  return undefined;
}

const ENTITIES = { "&amp;": "&", "&lt;": "<", "&gt;": ">", "&quot;": '"', "&#x27;": "'", "&#39;": "'" };
const decode = (s) => (s.includes("&") ? s.replace(/&(?:amp|lt|gt|quot|#x27|#39);/g, (e) => ENTITIES[e]) : s);

/**
 * Scans hljs's HTML into tokens per line.
 * @param {string} html
 * @returns {import("../domain/ports.js").Token[][]}
 */
export function scan(html) {
  const lines = [[]];
  const stack = [""]; // the token in force at each depth
  const push = (text) => {
    const tok = stack.at(-1);
    const parts = text.split("\n");
    parts.forEach((part, i) => {
      if (i > 0) lines.push([]);
      if (!part) return;
      const line = lines.at(-1);
      const last = line.at(-1);
      if (last && last.tok === tok) last.text += part;
      else line.push({ text: part, tok });
    });
  };
  let i = 0;
  while (i < html.length) {
    const lt = html.indexOf("<", i);
    if (lt < 0) {
      push(decode(html.slice(i)));
      break;
    }
    if (lt > i) push(decode(html.slice(i, lt)));
    const gt = html.indexOf(">", lt);
    if (gt < 0) {
      push(decode(html.slice(lt)));
      break;
    }
    const tag = html.slice(lt + 1, gt);
    if (tag.startsWith("/")) {
      if (stack.length > 1) stack.pop();
    } else {
      const m = /class="([^"]*)"/.exec(tag);
      const tok = tokenOf(scopeOf(m ? m[1] : ""));
      stack.push(tok === undefined ? stack.at(-1) : tok);
    }
    i = gt + 1;
  }
  return lines;
}

/**
 * @param {{ highlight: Function, getLanguage: Function }} hljs  core, with HLJS_LANGUAGES registered
 * @returns {import("../domain/ports.js").Highlighter}
 */
export function hljsHighlighter(hljs) {
  const nameOf = (language) => {
    const name = LANGUAGES[language];
    return name && hljs.getLanguage(name) ? name : "";
  };
  return {
    supports: (language) => nameOf(language) !== "",
    tokenize(language, text) {
      const name = nameOf(language);
      if (!name) return text.split("\n").map((t) => (t ? [{ text: t, tok: "" }] : []));
      try {
        return scan(hljs.highlight(text, { language: name, ignoreIllegals: true }).value);
      } catch {
        return text.split("\n").map((t) => (t ? [{ text: t, tok: "" }] : []));
      }
    },
  };
}
