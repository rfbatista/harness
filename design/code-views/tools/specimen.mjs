// Writes the "Code views" section of design-system/specimen.html as static
// markup (the specimen runs no code for it), from the same fixtures and
// renderers as the mockups (src/kit.js). It replaces what sits between the
// code-views markers, and refreshes the scoped light/dark theme rules in the
// specimen's style element from tokens.json, so the section can draw light
// beside dark. Run from the repository root after changing the blocks or the
// tokens: node design/code-views/tools/specimen.mjs
import { readFileSync, writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const here = dirname(fileURLToPath(import.meta.url));
const root = join(here, "../../..");
const specimenPath = join(root, "design-system/specimen.html");
const tokens = JSON.parse(readFileSync(join(root, "design-system/tokens.json"), "utf8"));

// Run the kit with just enough of a browser for it to define its renderers.
const window = {};
const document = { addEventListener() {} };
new Function("window", "document", readFileSync(join(here, "../src/kit.js"), "utf8"))(window, document);
const KIT = window.KIT;
const DIFF = window.KIT_DIFF;
DIFF[6].hunks = [[1, 0, "", ["-// Package history lists a commit's changed files.", "-package history", "-", "-func files() {}"]]];
DIFF[6].deleted = 4;

const cell = (tag, html) => `<div class="[ flow ] [ flow-space-2xs ]"><p class="[ font-mono text-xs color-ink-faint ]">${tag}</p>${html}</div>`;
const box = (html, style = "") => `<div class="specimen-box"${style ? ` style="${style}"` : ""}>${html}</div>`;
const marks = ["added", "modified", "deleted", "renamed", "copied", "type_changed", "untracked", "contains"];
const letter = { added: "A", modified: "M", deleted: "D", renamed: "R", copied: "C", type_changed: "T", untracked: "U", contains: "" };

function panel() {
  return [
    cell("change-mark · zone-tag · segmented · crumbs", `<div class="[ cluster ] [ gutter-s ]">${marks.map((c) => `<span class="[ cluster ] [ gutter-2xs text-xs color-ink-muted ]"><span class="change-mark" data-change="${c}" title="${c}">${letter[c]}</span>${c}</span>`).join("")}</div>
      <div class="[ cluster ] [ gutter-xs ]">${["Code browsing", "Orchestration", "Web UI adapters", "Workspaces", "Web client", "Architecture", "Projects", "Notifications"].map((n, i) => `<span class="zone-tag" data-hue="${i}">${n}</span>`).join("")}<span class="zone-tag" data-hue="none">design-system</span><span class="zone-tag" data-kind="context" data-hue="3">Workspaces</span><span class="zone-tag" data-dot-only data-hue="1" aria-label="Orchestration"></span></div>
      <div class="[ cluster ] [ gutter-s ]"><nav class="segmented" aria-label="Code views"><a href="#" aria-current="page">Changes <span class="count">7</span></a><a href="#">Files</a><a href="#">Map</a></nav><div class="segmented" role="group" aria-label="Layout"><button type="button" aria-pressed="true">Unified</button><button type="button" aria-pressed="false">Split</button></div><nav class="segmented" aria-label="Scope"><a href="#" aria-disabled="true" title="The base branch main no longer exists">All</a><a href="#" aria-disabled="true" title="The base branch main no longer exists">Branch</a><a href="#" aria-current="page">Working</a></nav></div>
      ${box(`<div class="[ pad-s ]">${KIT.crumbs("internal/adapter/in/httpapi/code_handlers.go")}</div>`)}`),
    cell("code-source-bar · live · stale · ended · gone · a ref", box(["live", "stale", "fixed", "gone"].map((s) => KIT.sourceBar({ state: s })).join("") + KIT.sourceBar({ state: "fixed", kind: "repository", view: "files" }))),
    cell("file-tree · expanded, selected, focused, marks, zone dots · loading, symlink, submodule, deleted, long name", `<div class="[ cluster ] [ gutter-s ]" data-align="start">${box(KIT.tree({ focus: "internal/ports/code.go" }), "inline-size: 18rem")}${box(KIT.tree({ open: new Set(["web", "third_party", "internal"]), busy: new Set(["internal"]), selected: "", nodes: [["internal", "dir", "contains", null, []], ["third_party", "dir", null, null, [["xterm", "submodule", null]]], ["web", "dir", "contains", 4, [["alpine.js", "symlink", null, 4, "../node_modules/alpinejs/dist/module.esm.js"], ["files.go", "file", "deleted", 2], ["a_component_with_a_very_long_file_name_for_the_tree.js", "file", "added", 4]]]] }), "inline-size: 18rem")}</div>`),
    cell("diff-summary · all · worktree gone · base gone · empty", `<div class="[ cluster ] [ gutter-s ]" data-align="start">${box(KIT.diffSummary(), "inline-size: 20rem")}${box(KIT.diffSummary({ gone: true, scope: "branch", files: DIFF.filter((f) => f.status !== "untracked").slice(0, 4) }), "inline-size: 20rem")}${box(KIT.diffSummary({ baseGone: true, scope: "working", files: DIFF.filter((f) => ["untracked", "modified"].includes(f.status)) }), "inline-size: 20rem")}${box(KIT.diffSummary({ files: [] }), "inline-size: 20rem")}</div>`),
    cell("diff-view · unified, two hunks, target", box(KIT.diffFile({ i: 1 }).replace('data-kind="add"><span class="gutter"><span class="num"></span><a class="num" data-line="15"', 'data-kind="add" data-target><span class="gutter"><span class="num"></span><a class="num" data-line="15"'))),
    cell("diff-view · split", box(KIT.diffFile({ i: 1, layout: "split" }))),
    cell("diff-view · renamed · untracked · no newline at end of file", box(KIT.diffFile({ i: 3 }) + KIT.diffFile({ i: 2 }) + KIT.diffFile({ i: 0 }))),
    cell("diff-view · withheld binary · too large · loading · collapsed", box(KIT.diffFile({ i: 4 }) + KIT.diffFile({ i: 5 }) + KIT.diffFile({ i: 5, busy: true }) + KIT.diffFile({ i: 6 }))),
    cell("code-view · targeted range", box(KIT.codeView({ text: window.KIT_BROWSER.split("\n").slice(38, 54).join("\n"), from: 39, target: [45, 49] }), "display: flex")),
    cell("code-view · wrap · truncated", box(KIT.codeView({ wrap: true, target: [], text: window.KIT_BROWSER.split("\n").slice(12, 20).join("\n") }), "display: flex") + box(KIT.codeView({ path: "web/vendor/highlight.min.js", zone: 4, lang: "javascript", target: [], meta: "JavaScript · 2.4 MB", text: "var h=function(e){return e.replace(/[&<>]/g,function(c){return M[c]})};", notice: 'Showing the first 1 MiB · 24,310 of 61,002 lines. The rest is not loaded. Read it in your editor: <code class="path">web/vendor/highlight.min.js</code>' }), "display: flex")),
    cell("code-view · binary · empty · symlink · submodule · loading", `<div class="[ cluster ] [ gutter-s ]" data-align="start">${box(KIT.codeView({ path: "docs/assets/code-map.png", kind: "binary", zone: null, meta: "PNG · 48 KB" }), "inline-size: 20rem")}${box(KIT.codeView({ path: "web/.gitkeep", kind: "empty", zone: null, meta: "0 bytes" }), "inline-size: 16rem")}${box(KIT.codeView({ path: "web/vendor/alpine.js", kind: "symlink", zone: null, meta: "symlink" }), "inline-size: 20rem")}${box(KIT.codeView({ path: "third_party/xterm", kind: "submodule", zone: null, meta: "submodule" }), "inline-size: 18rem")}${box(KIT.codeView({ busy: true, zone: null, meta: "loading…" }), "inline-size: 16rem; block-size: 10rem; display: flex")}</div>`),
    cell("code-map · contexts, no context, unzoned · no zones", box(KIT.codeMap(), "container: code / inline-size") + box(KIT.codeMap({ empty: true }))),
    cell("split-view · default · data-expanded (the detail takes the whole width at every width)", `<div class="[ flow ] [ flow-space-s ]">${["", " data-expanded"].map((a) => box(`<div class="split-view"${a} style="block-size: 7rem"><div class="list divide-end" role="listbox" aria-label="Sessions"><div class="row" role="option" aria-selected="true"><span class="status" data-state="running">running</span><span class="title">Design system — code views</span><span class="meta">designer</span></div></div><section class="[ pad-m ]" aria-label="Session detail"><button type="button" class="button" data-variant="ghost" data-size="sm" data-split-back>Sessions</button><p class="[ text-sm color-ink-muted ]">Code tab${a ? ", expanded (⇧E): the session list is hidden" : ""}</p></section></div>`)).join("")}</div>`),
    cell("code-layout at 375px · data-main, back control", `<div class="specimen-phone"><div class="code-layout" data-main><aside class="side"></aside><section class="main"><button type="button" class="button" data-variant="ghost" data-size="sm" data-code-back>‹ Changed files</button>${KIT.diffView({ files: [DIFF[1], DIFF[5]] })}</section></div></div>`),
  ].join("\n");
}

// Unique ids per panel: the dark copy gets a suffix.
const dark = (html) => html.replace(/\b(id|aria-controls|aria-labelledby)="([^"]+)"/g, '$1="$2-dark"').replace(/href="#(diff-[^"]+|L\d+)"/g, 'href="#$1-dark"');
const section = `<!-- code-views:start (generated by design/code-views/tools/specimen.mjs) -->
      <section class="[ flow ] [ flow-space-m ]" id="code-views">
        <h2>Code views</h2>
        <p class="[ color-ink-muted ]">Reading code: <code>code-layout</code> (side + main, a size container), <code>code-source-bar</code>, <code>segmented</code>, <code>file-tree</code>, <code>diff-summary</code>, <code>diff-view</code>, <code>code-view</code>, <code>code-lines</code>, <code>crumbs</code>, <code>change-mark</code>, <code>zone-tag</code> and <code>code-map</code>, with the <code>tok-*</code> syntax classes. Code sits in the <code>code</code> well, mono 13px on a fixed 20px line. A letter, a sign or a word carries every state, and colour only repeats it. Light beside dark.</p>
        <div class="specimen-themes">
          <div class="[ flow ] [ flow-space-l ] specimen-theme" data-theme="light">${panel()}</div>
          <div class="[ flow ] [ flow-space-l ] specimen-theme" data-theme="dark">${dark(panel())}</div>
        </div>
      </section>
      <!-- code-views:end -->`;

const themes = ["light", "dark"].map((t) => `      [data-theme="${t}"]:not(:root) {\n        color-scheme: ${t};\n${Object.entries(tokens.themes[t].color).map(([k, v]) => `        --color-${k}: ${v};`).join("\n")}\n        --shadow-float: ${tokens.themes[t].shadow.float};\n        color: var(--color-ink);\n      }`).join("\n");
const styles = `      /* code-views:themes:start (generated by design/code-views/tools/specimen.mjs from tokens.json) */
      .specimen-themes { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(100%, 38rem), 1fr)); gap: var(--space-m); }
      .specimen-theme { min-inline-size: 0; padding: var(--space-l); border: var(--border-width) solid var(--color-line); border-radius: var(--radius-md); background: var(--color-canvas); }
      .specimen-box { overflow: hidden; border: var(--border-width) solid var(--color-line); border-radius: var(--radius-sm); background: var(--color-canvas); }
      .specimen-box .diff-view { padding: 0; }
      .specimen-phone { display: flex; inline-size: 375px; max-inline-size: 100%; block-size: 34rem; overflow: hidden; border: var(--border-width) solid var(--color-line-strong); border-radius: 14px; background: var(--color-canvas); }
${themes}
      /* code-views:themes:end */`;

let html = readFileSync(specimenPath, "utf8");
const swap = (src, start, end, text, fallback) => {
  const a = src.indexOf(start), b = src.indexOf(end);
  if (a >= 0 && b > a) return src.slice(0, a) + text + src.slice(b + end.length);
  return fallback(src, text);
};
html = swap(html, "<!-- code-views:start", "<!-- code-views:end -->", section, (src, text) => src.replace('      <section class="[ flow ] [ flow-space-m ]">\n        <h2>Command palette and toast</h2>', text + '\n\n      <section class="[ flow ] [ flow-space-m ]">\n        <h2>Command palette and toast</h2>'));
html = swap(html, "      /* code-views:themes:start", "/* code-views:themes:end */", styles, (src, text) => src.replace("    </style>", text + "\n    </style>"));
writeFileSync(specimenPath, html);
console.log(`specimen.html: code views section ${Math.round(section.length / 1024)} KB`);
