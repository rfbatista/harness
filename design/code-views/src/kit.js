// Mockup kit for the code views: fixtures, a toy highlighter that emits only
// the tok-* classes of the contract, and renderers that write the proposed
// block markup. Scaffolding for the Design tab only; the Web UI owns the real
// behaviour. Inlined into each page by tools/build.py.

const esc = (s) => s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");

// ── A toy highlighter: enough to show every token colour. ──────────────────
const KEYWORDS = {
  go: "package import func return if else for range var const type struct interface map chan go defer select case switch default break continue nil true false",
  javascript: "import export from const let var function return if else for of in new class extends async await this null undefined true false default",
};
const TYPES = "string int int64 bool error byte rune any context.Context";
function hl(text, lang) {
  if (lang === "markdown") {
    if (/^#{1,6} /.test(text)) return `<span class="tok-heading">${esc(text)}</span>`;
    return esc(text)
      .replace(/`([^`]+)`/g, '<span class="tok-string">`$1`</span>')
      .replace(/\*\*([^*]+)\*\*/g, '<span class="tok-heading">**$1**</span>')
      .replace(/\[([^\]]+)\]\(([^)]+)\)/g, '[$1](<span class="tok-link">$2</span>)');
  }
  if (lang === "plaintext") return esc(text);
  const kw = new Set((KEYWORDS[lang] || "").split(" "));
  const types = new Set(TYPES.split(" "));
  const re = /(\/\/.*$)|("(?:[^"\\]|\\.)*"|`[^`]*`|'(?:[^'\\]|\\.)*')|(\/(?![/*])(?:[^/\\\n]|\\.)+\/[gimsuy]*(?=[.,;)\s]))|(\b\d+(?:\.\d+)?\b)|([A-Za-z_][\w]*)(\s*\()?|([{}()[\];,.])|([-+*/%=!<>&|^:?]+)/g;
  let out = "", last = 0, m;
  while ((m = re.exec(text))) {
    out += esc(text.slice(last, m.index));
    last = re.lastIndex;
    const [all, comment, str, rx, num, id, call, punct, op] = m;
    if (comment) out += `<span class="tok-comment">${esc(comment)}</span>`;
    else if (str) out += `<span class="tok-string">${esc(str)}</span>`;
    else if (rx && lang === "javascript") out += `<span class="tok-regexp">${esc(rx)}</span>`;
    else if (num) out += `<span class="tok-number">${num}</span>`;
    else if (id) {
      const rest = esc(call || "");
      if (kw.has(id)) out += `<span class="${/^(true|false|nil|null|undefined)$/.test(id) ? "tok-constant" : "tok-keyword"}">${id}</span>${rest}`;
      else if (call) out += `<span class="tok-function">${id}</span>${rest}`;
      else if (types.has(id) || /^[A-Z]/.test(id)) out += `<span class="tok-type">${id}</span>`;
      else out += id;
    } else if (punct) out += `<span class="tok-punctuation">${esc(punct)}</span>`;
    else if (op) out += `<span class="tok-operator">${esc(op)}</span>`;
    else out += esc(all);
  }
  return out + esc(text.slice(last));
}

// ── Fixtures ───────────────────────────────────────────────────────────────
const BROWSER_GO = `package code

import (
	"context"
	"errors"
	"path"
	"strings"

	"github.com/renan/harness/internal/domain"
	"github.com/renan/harness/internal/ports"
)

// MaxFileBytes caps what a file read returns; past it the content is cut at
// a line end and the answer says truncated.
const MaxFileBytes = 1 << 20

// ErrPathNotFound is PATH_NOT_FOUND on the wire.
var ErrPathNotFound = errors.New("PATH_NOT_FOUND: no such path in the source")

// Browser reads code for the web UI: trees, files, diffs and the map.
// It never writes to a repository or a worktree.
type Browser struct {
	git   ports.GitReader
	zones ports.ZoneMatcher
}

// NewBrowser wires the read ports the use cases need.
func NewBrowser(git ports.GitReader, zones ports.ZoneMatcher) *Browser {
	return &Browser{git: git, zones: zones}
}

// File answers one file of a source, capped at MaxFileBytes.
func (b *Browser) File(ctx context.Context, src domain.CodeSource, p string) (domain.CodeFile, error) {
	if err := checkPath(p); err != nil {
		return domain.CodeFile{}, err
	}
	blob, err := b.git.ReadBlob(ctx, src, p)
	if err != nil {
		return domain.CodeFile{}, err
	}
	file := domain.CodeFile{Source: src, Path: p, Size: len(blob.Data)}
	if blob.Binary {
		file.Binary = true
		return file, nil
	}
	if len(blob.Data) > MaxFileBytes {
		cut := strings.LastIndexByte(string(blob.Data[:MaxFileBytes]), '\\n')
		blob.Data, file.Truncated = blob.Data[:cut+1], true
	}
	file.Content = string(blob.Data)
	file.LineCount = strings.Count(file.Content, "\\n")
	file.Zones = b.zones.Match(src.ProjectID, p)
	return file, nil
}

// checkPath refuses what could leave the source's root.
func checkPath(p string) error {
	if p == "" || strings.HasPrefix(p, "/") || strings.ContainsRune(p, 0) {
		return domain.ErrInvalidInput
	}
	for _, seg := range strings.Split(path.Clean(p), "/") {
		if seg == ".." {
			return domain.ErrInvalidInput
		}
	}
	return nil
}`;

const TREE_JS = `import { codeGateway } from "../infrastructure/gateway.js";

// One directory level at a time: the tree is lazy.
export function tree(source) {
  return {
    rows: [],
    busy: new Set(),
    async expand(dir) {
      this.busy.add(dir.path);
      const { entries } = await codeGateway.tree(source, dir.path);
      dir.children = entries.filter((e) => !/^\\.git$/.test(e.name));
      this.busy.delete(dir.path);
    },
  };
}`;

// A tree: [name, kind, change, zoneHue, children|target]
const TREE = [
  ["cmd", "dir", null],
  ["design-system", "dir", "contains"],
  ["docs", "dir", "contains", null, [
    ["assets", "dir", "contains", null, [["code-map.png", "file", "added"]]],
    ["WEB.md", "file", null],
  ]],
  ["internal", "dir", "contains", null, [
    ["adapter", "dir", "contains", 2],
    ["application", "dir", "contains", null, [
      ["code", "dir", "contains", 0, [
        ["browser.go", "file", "modified", 0],
        ["browser_test.go", "file", "untracked", 0],
        ["diff.go", "file", null, 0],
      ]],
      ["orchestration", "dir", null, 1],
    ]],
    ["domain", "dir", null, 3],
    ["ports", "dir", "contains", 3, [
      ["code.go", "file", null, 3],
      ["workspaces.go", "file", "renamed", 3],
    ]],
  ]],
  ["third_party", "dir", null, null, [["xterm", "submodule", null]]],
  ["web", "dir", "contains", 4, [
    ["vendor", "dir", null, 4, [["highlight.min.js", "file", "added", 4], ["alpine.js", "symlink", null, 4, "../node_modules/alpinejs/dist/module.esm.js"]]],
  ]],
  ["DESIGN.md", "file", "modified"],
  ["go.mod", "file", null],
  ["Makefile", "file", null],
  ["README.md", "file", null],
];
const CONTEXTS = ["Code browsing", "Orchestration", "Web UI adapters", "Workspaces", "Web client"];

const MARK = { added: "A", modified: "M", deleted: "D", renamed: "R", copied: "C", type_changed: "T", untracked: "U", contains: "" };
const WORD = { added: "Added", modified: "Modified", deleted: "Deleted", renamed: "Renamed", copied: "Copied", type_changed: "Type changed", untracked: "Untracked", contains: "Contains changes" };
const mark = (c) => (c ? `<span class="change-mark" data-change="${c}" title="${WORD[c]}" aria-label="${WORD[c]}">${MARK[c]}</span>` : "");

const ICON = {
  dir: '<svg class="icon" aria-hidden="true" viewBox="0 0 16 16"><path d="M2 4.25h4.25l1.25 1.5H14v6.5H2z" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round"/></svg>',
  file: '<svg class="icon" aria-hidden="true" viewBox="0 0 16 16"><path d="M4 1.75h5.25L12.25 4.75v9.5H4z M9 1.75V5h3.25" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round"/></svg>',
  symlink: '<svg class="icon" aria-hidden="true" viewBox="0 0 16 16"><path d="M4 1.75h5.25L12.25 4.75v9.5H4z M6.5 11.5c0-2 1-3 3-3M8.5 7l1.25 1.5L8.5 10" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round" stroke-linecap="round"/></svg>',
  submodule: '<svg class="icon" aria-hidden="true" viewBox="0 0 16 16"><path d="M2 4.25h4.25l1.25 1.5H14v6.5H2z M6 9h4" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round"/></svg>',
};
const CHEVRON = '<svg class="chevron" aria-hidden="true" viewBox="0 0 12 12"><path d="M3 4.5 6 7.5 9 4.5" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/></svg>';

function treeItems(nodes, depth, parent, opts) {
  return nodes.map(([name, kind, change, hue, rest]) => {
    const p = parent ? `${parent}/${name}` : name;
    const isDir = kind === "dir";
    const children = Array.isArray(rest) ? rest : null;
    const open = isDir && children && opts.open.has(p);
    const busy = isDir && opts.busy?.has(p);
    const sel = opts.selected === p;
    const zone = hue != null && depth > 0 && !isDir ? `<span class="zone-tag" data-dot-only data-hue="${hue}" title="${CONTEXTS[hue]}" aria-label="${CONTEXTS[hue]}"></span>` : "";
    const target = kind === "symlink" ? `<span class="target">${esc(rest)}</span>` : "";
    const tag = isDir || kind === "submodule" ? "span" : "a";
    const href = tag === "a" ? ` href="#${p}"` : "";
    const focus = opts.focus === p ? ` tabindex="0" data-demo-focus` : ` tabindex="-1"`;
    if (opts.marks === false) change = null;
    const item = `<${tag} class="item"${href}${focus}>${isDir ? CHEVRON : "<span></span>"}${ICON[kind]}<span class="name">${esc(name)}${target}</span>${zone}${mark(change)}</${tag}>`;
    let group = "";
    if (open) group = `<ul role="group">${treeItems(children, depth + 1, p, opts)}</ul>`;
    if (busy) group = `<ul role="group">${[62, 48, 70].map((w) => `<li class="skeleton-row" style="--depth:${depth + 1}" aria-hidden="true"><span class="skeleton" style="--skeleton-width:${w}%"></span></li>`).join("")}</ul>`;
    const attrs = [
      `role="treeitem"`, `aria-level="${depth + 1}"`, `style="--depth:${depth}"`, `data-kind="${kind}"`, `data-path="${p}"`, `title="${p}"`,
      isDir ? `aria-expanded="${open || busy ? "true" : "false"}"` : "",
      busy ? `aria-busy="true"` : "",
      sel ? `aria-selected="true"` : "",
      change === "deleted" ? `data-change="deleted"` : "",
    ].filter(Boolean).join(" ");
    return `<li ${attrs}>${item}${group}</li>`;
  }).join("");
}
function tree(opts = {}) {
  opts = { open: new Set(["internal", "internal/application", "internal/application/code", "internal/ports"]), selected: "internal/application/code/browser.go", ...opts };
  return `<ul class="file-tree" role="tree" aria-label="Files">${treeItems(opts.nodes || TREE, 0, "", opts)}</ul>`;
}

// ── Code lines ─────────────────────────────────────────────────────────────
function codeLines(text, { lang = "go", target = [], from = 1, wrap = false, prefix = "L" } = {}) {
  const lines = text.split("\n");
  const digits = String(from + lines.length - 1).length;
  const rows = lines.map((t, i) => {
    const n = from + i;
    const tg = n >= target[0] && n <= (target[1] || target[0]) ? " data-target" : "";
    return `<div class="line" id="${prefix}${n}"${tg}><span class="gutter"><a class="num" data-line="${n}" href="#${prefix}${n}" tabindex="-1" aria-label="Line ${n}"></a></span><span class="text">${hl(t, lang)}</span></div>`;
  });
  return `<div class="code-lines" style="--digits:${digits}"${wrap ? " data-wrap" : ""}>${rows.join("")}</div>`;
}

// ── Diffs ──────────────────────────────────────────────────────────────────
// A hunk: [oldStart, newStart, header, lines] where a line is " text", "+text" or "-text".
const DIFF = [
  { path: "DESIGN.md", status: "modified", lang: "markdown", zone: null, hunks: [[324, 324, "### Transcript (signature)", [
    " The `sunken` well where a session's output is read: mono 13px, a grid of `time | mark | text`.",
    " ",
    "+### Code views",
    "+Reading code is the densest surface: **mono 13px on a 20px line**, the `code` well, and colour only where it gives code its shape.",
    "+- **File tree** (`.file-tree`): a lazy ARIA tree; change marks are letters, never colour alone.",
    "+- **Diff view** (`.diff-view`): unified by default, split from 44rem; see [the contract](docs/code-views.md).",
  ]]], noNewline: true },
  { path: "internal/application/code/browser.go", status: "modified", lang: "go", zone: 0, hunks: [
    [13, 13, "", [
      " // MaxFileBytes caps what a file read returns; past it the content is cut at",
      " // a line end and the answer says truncated.",
      "-const MaxFileBytes = 1 << 19",
      "+const MaxFileBytes = 1 << 20",
      " ",
      " // ErrPathNotFound is PATH_NOT_FOUND on the wire.",
      " var ErrPathNotFound = errors.New(\"PATH_NOT_FOUND: no such path in the source\")",
    ]],
    [45, 45, "func (b *Browser) File(ctx context.Context, src domain.CodeSource, p string) (domain.CodeFile, error) {", [
      " \tif blob.Binary {",
      " \t\tfile.Binary = true",
      " \t\treturn file, nil",
      " \t}",
      "-\tfile.Content = string(blob.Data)",
      "+\tif len(blob.Data) > MaxFileBytes {",
      "+\t\tcut := strings.LastIndexByte(string(blob.Data[:MaxFileBytes]), '\\n')",
      "+\t\tblob.Data, file.Truncated = blob.Data[:cut+1], true",
      "+\t}",
      "+\tfile.Content = string(blob.Data)",
      " \tfile.LineCount = strings.Count(file.Content, \"\\n\")",
      " \tfile.Zones = b.zones.Match(src.ProjectID, p)",
      " \treturn file, nil",
    ]],
  ] },
  { path: "internal/application/code/browser_test.go", status: "untracked", lang: "go", zone: 0, hunks: [[0, 1, "", [
    "+package code_test",
    "+",
    "+import \"testing\"",
    "+",
    "+func TestFileIsCutAtALineEndPastTheCap(t *testing.T) {",
    "+\tsrc := fixtureSource(t, \"big.txt\", 2<<20)",
    "+\tfile, err := browser(t).File(ctx, src, \"big.txt\")",
    "+\tif err != nil || !file.Truncated {",
    "+\t\tt.Fatalf(\"want a truncated file, got %v %v\", file.Truncated, err)",
    "+\t}",
    "+}",
  ]]] },
  { path: "internal/ports/workspaces.go", old: "internal/ports/workspace.go", status: "renamed", lang: "go", zone: 3, hunks: [[8, 8, "type GitReader interface {", [
    " \tLog(ctx context.Context, repo string, limit int) ([]Commit, error)",
    "-\tShow(ctx context.Context, repo, hash string) (Commit, error)",
    "+\tShow(ctx context.Context, repo, hash string) (Commit, error)",
    "+\tReadBlob(ctx context.Context, src domain.CodeSource, path string) (Blob, error)",
    " }",
  ]]] },
  { path: "docs/assets/code-map.png", status: "added", binary: true, withheld: "binary", zone: null, size: "48 KB" },
  { path: "web/vendor/highlight.min.js", status: "added", withheld: "too_large", added: 4812, deleted: 0, zone: 4 },
  { path: "internal/adapter/in/web/history/files.go", status: "deleted", lang: "go", zone: 2, collapsed: true, hunks: [[1, 0, "", Array.from({ length: 88 }, (_, i) => "-// line " + (i + 1))]] },
];
for (const f of DIFF) {
  if (f.hunks) {
    f.added = f.hunks.reduce((n, h) => n + h[3].filter((l) => l[0] === "+").length, 0);
    f.deleted = f.hunks.reduce((n, h) => n + h[3].filter((l) => l[0] === "-").length, 0);
  }
}
const slug = (p) => "diff-" + p.replace(/[^\w]+/g, "-");

function hunkRows(f, layout) {
  const out = [];
  for (const [os, ns, header, lines] of f.hunks) {
    const oc = lines.filter((l) => l[0] !== "+").length, nc = lines.filter((l) => l[0] !== "-").length;
    out.push(`<div class="hunk"><span class="range">@@ -${os},${oc} +${ns},${nc} @@</span>${header ? `<span class="context">${hl(header, f.lang)}</span>` : ""}</div>`);
    let o = os, n = ns;
    const parsed = lines.map((l) => {
      const kind = l[0] === "+" ? "add" : l[0] === "-" ? "del" : "context";
      const r = { kind, text: l.slice(1), o: kind === "add" ? null : o, n: kind === "del" ? null : n };
      if (kind !== "add") o++;
      if (kind !== "del") n++;
      return r;
    });
    const num = (v, side) => (v == null ? `<span class="num"></span>` : `<a class="num" data-line="${v}" href="#${slug(f.path)}-${side}${v}" tabindex="-1" aria-label="${side === "L" ? "Old" : "New"} line ${v}"></a>`);
    const last = parsed.length - 1;
    if (layout === "split") {
      // Pair runs of deletions with the additions that follow them.
      for (let i = 0; i < parsed.length; ) {
        const r = parsed[i];
        if (r.kind === "context") {
          const cell = (v, side) => `<div class="half" data-kind="context"><span class="gutter">${num(v, side)}</span><span class="text">${hl(r.text, f.lang)}</span></div>`;
          out.push(`<div class="line">${cell(r.o, "L")}${cell(r.n, "R")}</div>`);
          i++;
          continue;
        }
        const dels = [], adds = [];
        while (i < parsed.length && parsed[i].kind === "del") dels.push(parsed[i++]);
        while (i < parsed.length && parsed[i].kind === "add") adds.push(parsed[i++]);
        for (let k = 0; k < Math.max(dels.length, adds.length); k++) {
          const d = dels[k], a = adds[k];
          const L = d ? `<div class="half" data-kind="del"><span class="gutter">${num(d.o, "L")}</span><span class="text">${hl(d.text, f.lang)}</span></div>` : `<div class="half" data-kind="empty"></div>`;
          const R = a ? `<div class="half" data-kind="add"><span class="gutter">${num(a.n, "R")}</span><span class="text">${hl(a.text, f.lang)}</span></div>` : `<div class="half" data-kind="empty"></div>`;
          out.push(`<div class="line">${L}${R}</div>`);
        }
      }
    } else {
      parsed.forEach((r, i) => {
        const nn = f.noNewline && i === last ? " data-no-newline" : "";
        out.push(`<div class="line" data-kind="${r.kind}"${nn}><span class="gutter">${num(r.o, "L")}${num(r.n, "R")}</span><span class="text">${hl(r.text, f.lang)}</span></div>`);
      });
    }
  }
  return out.join("");
}

const ICON_FILE_BIN = '<svg aria-hidden="true" viewBox="0 0 16 16"><path d="M4 1.75h5.25L12.25 4.75v9.5H4z M9 1.75V5h3.25 M6.5 8.5v3 M9.5 8.5v3" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round"/></svg>';
function diffFile(f, { layout = "unified", busy = false } = {}) {
  const id = slug(f.path);
  const zone = f.zone != null ? `<span class="zone-tag" data-hue="${f.zone}">${CONTEXTS[f.zone]}</span>` : "";
  const stat = f.binary ? `<span class="stat"><span class="note">binary · ${f.size}</span></span>` : `<span class="stat"><span class="added">+${f.added.toLocaleString("en")}</span><span class="deleted">−${f.deleted.toLocaleString("en")}</span></span>`;
  const viewFile = f.status === "deleted" || f.binary ? "" : `<a class="button" data-variant="ghost" data-size="sm" href="#">View file</a>`;
  const head = `<header class="head"><button type="button" class="toggle" aria-expanded="${f.collapsed ? "false" : "true"}" aria-controls="${id}-body" aria-label="${f.collapsed ? "Expand" : "Collapse"} ${f.path.split("/").pop()}">${CHEVRON.replace('class="chevron" ', "")}</button>${mark(f.status)}<h3 class="path" id="${id}-name" title="${f.path}">${f.old ? `<span class="old">${f.old}</span>` : ""}${f.path}</h3>${zone}${stat}<div class="actions">${viewFile}</div></header>`;
  let body;
  if (f.withheld === "binary") body = `<div class="withheld" data-reason="binary" id="${id}-body">${ICON_FILE_BIN}<p><strong>Binary file</strong> · ${f.size}. Its changes are not shown as text.</p></div>`;
  else if (f.withheld === "too_large") body = `<div class="withheld" data-reason="too_large" id="${id}-body"><p><strong>Large diff, not loaded</strong> · ${f.added.toLocaleString("en")} changed lines. Loading it may slow this page.</p><button type="button" class="button" data-size="sm"${busy ? ' aria-busy="true"' : ""}>Load diff</button></div>`;
  else body = `<div class="hunks" id="${id}-body"><div class="code-lines" style="--digits:3"${layout === "split" ? ' data-layout="split"' : ""}>${hunkRows(f, layout)}</div></div>`;
  return `<section class="file" id="${id}" aria-labelledby="${id}-name"${f.collapsed ? " data-collapsed" : ""}>${head}${body}</section>`;
}
function diffView({ layout = "unified", files = DIFF } = {}) {
  const sw = `<div class="segmented" data-layout-switch role="group" aria-label="Diff layout"><button type="button" aria-pressed="${layout === "unified"}" data-layout="unified">Unified</button><button type="button" aria-pressed="${layout === "split"}" data-layout="split">Split</button></div>`;
  return `<div class="diff-view"><div class="bar"><button type="button" class="button" data-variant="ghost" data-size="sm">Collapse all</button><span class="color-ink-faint text-xs">Files <kbd>[</kbd> <kbd>]</kbd></span>${sw}</div>${files.map((f) => diffFile(f, { layout })).join("")}</div>`;
}
const totals = (files = DIFF) => ({ n: files.length, a: files.reduce((s, f) => s + (f.binary ? 0 : f.added), 0), d: files.reduce((s, f) => s + (f.binary ? 0 : f.deleted), 0) });
function diffSummary({ scope = "all", current = DIFF[1].path, files = DIFF, gone = false, baseGone = false } = {}) {
  const t = totals(files);
  const scopes = [["all", "All"], ["branch", "Branch"], ["working", "Working"]].map(([k, l]) => {
    const why = gone && k !== "branch" ? "The worktree is gone: only committed work remains" : baseGone && k !== "working" ? "The base branch main no longer exists" : "";
    return `<a href="#scope=${k}"${scope === k ? ' aria-current="page"' : ""}${why ? ` aria-disabled="true" title="${why}"` : ""}>${l}</a>`;
  }).join("");
  const rows = files.map((f) => {
    const parts = f.path.split("/"), name = parts.pop(), dir = parts.length ? parts.join("/") + "/" : "";
    const stat = f.binary ? `<span class="note">binary</span>` : `<span class="added">+${f.added.toLocaleString("en")}</span><span class="deleted">−${f.deleted}</span>${f.withheld ? '<span class="note">not loaded</span>' : ""}`;
    return `<li><a href="#${slug(f.path)}"${f.path === current ? ' aria-current="location"' : ""}>${mark(f.status)}<span class="path"><bdi><span class="dir">${dir}</span><span class="name">${name}</span></bdi></span><span class="stat">${stat}</span>${f.old ? `<span class="old">${f.old}</span>` : ""}</a></li>`;
  }).join("");
  return `<nav class="diff-summary" aria-label="Changed files"><header class="head"><p class="totals">${t.n} files <span class="added">+${t.a.toLocaleString("en")}</span> <span class="deleted">−${t.d}</span></p><nav class="segmented" aria-label="Scope">${scopes}</nav></header>${files.length ? `<ol class="changed">${rows}</ol>` : `<p class="none">No changes on this branch yet. The agent's edits appear here as it makes them.</p>`}</nav>`;
}

// ── Code view ──────────────────────────────────────────────────────────────
function crumbs(path, repo = "harness") {
  const parts = path.split("/");
  const lis = [`<li><a href="#">${repo}</a></li>`, ...parts.map((p, i) => (i === parts.length - 1 ? `<li><a aria-current="page">${p}</a></li>` : `<li><a href="#">${p}</a></li>`))];
  return `<nav class="crumbs" aria-label="Path"><ol>${lis.join("")}</ol></nav>`;
}
const ICON_WRAP = '<svg aria-hidden="true" viewBox="0 0 16 16"><path d="M2 4h12M2 8h9.5a2 2 0 010 4H8m0 0l1.5-1.5M8 12l1.5 1.5M2 12h3" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/></svg>';
const ICON_LINK = '<svg aria-hidden="true" viewBox="0 0 16 16"><path d="M7 9a2.5 2.5 0 003.5 0l2-2A2.5 2.5 0 009 3.5l-.5.5M9 7a2.5 2.5 0 00-3.5 0l-2 2A2.5 2.5 0 007 12.5l.5-.5" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/></svg>';
function codeView({ path = "internal/application/code/browser.go", text = BROWSER_GO, lang = "go", target = [45, 49], wrap = false, from = 1, kind = "text", zone = 0, meta, notice, busy } = {}) {
  const lines = text.split("\n").length;
  meta = meta || `${lang === "go" ? "Go" : lang} · ${lines} lines · ${(text.length / 1024).toFixed(1)} KB`;
  const actions = `<div class="actions"><button type="button" class="button" data-variant="ghost" data-size="sm" data-icon aria-pressed="${wrap}" aria-label="Wrap lines (W)" title="Wrap lines  W">${ICON_WRAP}</button><button type="button" class="button" data-variant="ghost" data-size="sm" data-icon aria-label="Copy a link to this line (Y)" title="Copy link  Y">${ICON_LINK}</button><a class="button" data-variant="ghost" data-size="sm" href="#">Changes</a></div>`;
  const z = zone != null ? `<span class="zone-tag" data-hue="${zone}">${CONTEXTS[zone]}</span>` : "";
  let body;
  if (busy) body = `<div class="scroll">${[38, 62, 54, 20, 71, 44, 66, 30].map((w) => `<span class="skeleton" style="--skeleton-width:${w}%"></span>`).join("")}</div>`;
  else if (kind === "binary") body = `<div class="placeholder">${ICON_FILE_BIN}<strong>Binary file, not shown</strong><p>code-map.png · 48 KB</p></div>`;
  else if (kind === "symlink") body = `<div class="placeholder">${ICON.symlink.replace('class="icon" ', "")}<strong>Symbolic link</strong><p>Points to <code>../node_modules/alpinejs/dist/module.esm.js</code>. Links are not followed.</p><a class="button" data-size="sm" href="#">Open the target</a></div>`;
  else if (kind === "submodule") body = `<div class="placeholder">${ICON.submodule.replace('class="icon" ', "")}<strong>Submodule, not read into</strong><p>third_party/xterm is another repository.</p></div>`;
  else if (kind === "empty") body = `<div class="placeholder">${ICON.file.replace('class="icon" ', "")}<strong>Empty file</strong><p>.gitkeep has no content.</p></div>`;
  else body = `<div class="scroll">${codeLines(text, { lang, target, wrap, from })}</div>`;
  return `<article class="code-view"${busy ? ' aria-busy="true"' : ""}><header class="head">${crumbs(path)}${z}<span class="meta">${meta}</span>${kind === "text" && !busy ? actions : ""}</header>${notice ? `<p class="notice">${notice}</p>` : ""}${body}</article>`;
}

// ── Source bar ─────────────────────────────────────────────────────────────
function sourceBar({ view = "changes", state = "live", kind = "session", counts = true } = {}) {
  const t = totals();
  const views = [["changes", "Changes", kind === "session" ? `<span class="count">${t.n}</span>` : null], ["files", "Files"], ["map", "Map"]]
    .filter(([k]) => kind === "session" || k !== "changes")
    .map(([k, l, c]) => `<a href="#view=${k}" data-view="${k}"${view === k ? ' aria-current="page"' : ""}>${l}${c && counts ? " " + c : ""}</a>`).join("");
  const source = kind === "session"
    ? `<div class="origin"><span class="repo">harness</span><span class="branch" title="agent/visualize-code-in-the-web-ui-9891f66e">agent/visualize-code-in-the-web-ui-9891f66e</span><code class="commit">${state === "gone" ? "c741e99" : "3f9a2c1"}</code>${state === "gone" ? "" : '<span class="dirty">3 uncommitted</span>'}</div>`
    : `<div class="origin"><span class="repo">harness</span><select data-size="sm" data-fit aria-label="Branch, tag or commit"><option>main</option><option>agent/visualize-code-in-the-web-ui-9891f66e</option><option>v0.9.0</option></select><code class="commit">c741e99</code></div>`;
  const stateEl = {
    live: '<p class="state" role="status">live</p>',
    stale: '<p class="state" role="status">Changed on disk</p>',
    gone: '<p class="state" role="status">worktree gone</p>',
    fixed: "",
  }[state];
  const reload = state === "stale" ? '<button type="button" class="button" data-size="sm" data-reload>Reload <kbd>R</kbd></button>' : "";
  const find = `<button type="button" class="button" data-variant="ghost" data-size="sm">Find file <kbd>T</kbd></button>`;
  const gone = state === "gone" ? `<div class="gone" role="note"><span><strong>This session's worktree was removed.</strong> Showing what it committed on its branch; uncommitted work is gone with the worktree.</span><span class="code">WORKSPACE_MISSING</span><a class="button" data-size="sm" href="#">Browse the branch</a></div>` : "";
  return `<header class="code-source-bar"${state !== "fixed" ? ` data-state="${state}"` : ""}><nav class="segmented" aria-label="Code views">${views}</nav>${source}${stateEl}<div class="actions">${reload}${find}</div>${gone}</header>`;
}

// ── Map ────────────────────────────────────────────────────────────────────
const MAP = {
  file_count: 1284, unzoned: 212,
  contexts: [
    { name: "Code browsing", purpose: "Reads trees, files, diffs and zone matches for the web UI. Never writes.", zones: [["code", "internal/application/code/**", 18], ["code-http", "internal/adapter/in/httpapi/code*.go", 4]] },
    { name: "Orchestration", purpose: "Runs agent sessions, their terminals and the project feed.", zones: [["orchestration", "internal/application/orchestration/**", 96], ["terminal", "internal/adapter/out/pty/**", 22], ["feed", "internal/application/feed/**", 31]] },
    { name: "Web UI adapters", purpose: "templ pages and the BFF that renders first paint in process.", zones: [["web pages", "internal/adapter/in/web/**", 286], ["components", "internal/adapter/in/web/components/**", 12]] },
    { name: "Workspaces", purpose: "Repositories, worktrees and every git read.", zones: [["git", "internal/adapter/out/git/**", 64], ["workspaces", "internal/application/workspaces/**", 41], ["ports", "internal/ports/**", 38]] },
    { name: "Web client", purpose: "The browser modules over /api: domain, infrastructure, presentation.", zones: [["web modules", "web/src/**", 341], ["vendor", "web/vendor/**", 0]] },
  ],
  unassigned: [["design-system", "design-system/**", 118], ["docs", "docs/**", 27]],
};
function codeMap({ empty = false } = {}) {
  if (empty) return `<div class="code-map"><div class="empty"><h2>No zones in this project yet</h2><p>Zones map paths to the parts of your architecture, and bounded contexts group them. Once the project has some, this map shows how its 1,284 files divide between them.</p><div class="actions"><a class="button" data-variant="primary" href="#">Add zones</a><a class="button" data-variant="ghost" href="#">Browse files</a></div></div></div>`;
  const ctxCount = (c) => c.zones.reduce((s, z) => s + z[2], 0);
  const most = Math.max(...MAP.contexts.flatMap((c) => c.zones.map((z) => z[2])), ...MAP.unassigned.map((z) => z[2]));
  const zoneRows = (zs) => zs.map(([n, pat, c]) => `<li><a href="#"${c === 0 ? " data-empty" : ""}><span class="name">${n}</span><code class="pattern">${pat}</code><span class="meter" style="--share:${(c / most).toFixed(3)}" aria-hidden="true"></span><span class="count">${c === 0 ? "no files" : c}</span></a></li>`).join("");
  const spread = [...MAP.contexts.map((c, i) => `<span data-hue="${i}" style="--share:${ctxCount(c)}" title="${c.name}: ${ctxCount(c)} files"></span>`),
    `<span data-hue="none" style="--share:${MAP.unassigned.reduce((s, z) => s + z[2], 0)}" title="No context"></span>`,
    `<span data-kind="unzoned" style="--share:${MAP.unzoned}" title="Unzoned: ${MAP.unzoned} files"></span>`].join("");
  const cards = MAP.contexts.map((c, i) => `<section class="context" data-hue="${i}" aria-labelledby="ctx-${i}"><header><h3 id="ctx-${i}"><span class="zone-tag" data-kind="context" data-hue="${i}">${c.name}</span></h3><span class="count">${ctxCount(c)} files</span><p class="purpose">${c.purpose}</p></header><ol class="zones">${zoneRows(c.zones)}</ol></section>`).join("");
  const unassigned = `<section class="context" data-kind="unassigned" data-hue="none" aria-labelledby="ctx-none"><header><h3 id="ctx-none"><span class="zone-tag" data-kind="context" data-hue="none">No bounded context</span></h3><span class="count">${MAP.unassigned.reduce((s, z) => s + z[2], 0)} files</span><p class="purpose">Zones that no bounded context claims yet.</p></header><ol class="zones">${zoneRows(MAP.unassigned)}</ol></section>`;
  const unzoned = `<section class="context" data-kind="unzoned" aria-labelledby="ctx-unzoned"><header><h3 id="ctx-unzoned">Unzoned files</h3><span class="count">${MAP.unzoned} files</span><p class="purpose">No zone pattern or explicit path matches them. <a href="#">Browse them</a></p></header></section>`;
  return `<div class="code-map"><header class="overview"><p class="totals"><strong>${MAP.file_count.toLocaleString("en")}</strong> files · ${MAP.contexts.length} bounded contexts · ${MAP.contexts.reduce((s, c) => s + c.zones.length, 0) + MAP.unassigned.length} zones · ${MAP.unzoned} unzoned</p><div class="spread" aria-hidden="true">${spread}</div></header><div class="contexts">${cards}${unassigned}${unzoned}</div></div>`;
}

// ── Page helpers ───────────────────────────────────────────────────────────
function fill(root = document) {
  root.querySelectorAll("[data-render]").forEach((el) => {
    const opts = el.dataset.opts ? JSON.parse(el.dataset.opts) : {};
    el.innerHTML = KIT[el.dataset.render](opts);
  });
}
const KIT = { tree, codeLines, codeView, diffView, diffFile: (o) => `<div class="diff-view" style="padding:0">${diffFile(DIFF[o.i ?? 1], o)}</div>`, diffSummary, sourceBar, codeMap, crumbs };
window.KIT = KIT;
window.KIT_DIFF = DIFF;
window.KIT_BROWSER = BROWSER_GO;
window.KIT_fill = fill;
document.addEventListener("DOMContentLoaded", () => fill());
