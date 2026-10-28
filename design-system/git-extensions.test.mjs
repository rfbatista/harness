// Invariants of the extensions of the code-views contract, revision 6, and of the session row contract,
// revision 2, that text can check: diff-summary (legend, groups, sections, staged flag, updated dot),
// diff-view (notice, "updated N s ago"), segmented (count), code-source-bar (chip slot, stopped note,
// narrow-bar priority) and the session row chip slot. Pixel layout is proven in a browser, not here.
// Run: node --test design-system/git-extensions.test.mjs
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const read = (p) => readFileSync(fileURLToPath(new URL(p, import.meta.url)), "utf8");
const strip = (css) => css.replace(/\/\*[\s\S]*?\*\//g, "");
const FILES = ["diff-summary", "diff-view", "segmented", "code-source-bar", "row"];
const code = Object.fromEntries(FILES.map((b) => [b, strip(read(`./css/blocks/${b}.css`))]));
const tokens = read("./css/global/tokens.css");
const specimen = read("./specimen.html");
const design = read("../DESIGN.md");

const section = (() => {
  const start = specimen.indexOf('id="git-changes"');
  return start < 0 ? "" : specimen.slice(start, specimen.indexOf("<!-- code-views:start", start));
})();
const half = (theme) => section.split(/(?=<div[^>]*specimen-theme)/).find((h) => h.includes(`data-theme="${theme}"`)) ?? "";

test("tokens only: no colour literal in the extended blocks, every var() is a token or private", () => {
  const defined = new Set([...tokens.matchAll(/(--[\w-]+)\s*:/g)].map((m) => m[1]));
  for (const b of FILES) {
    assert.doesNotMatch(code[b], /#[0-9a-f]{3,8}\b|\b(rgb|rgba|hsl|hsla|lab|lch)\(/i, `${b}: a colour literal`);
    const local = new Set([...code[b].matchAll(/(--[\w-]+)\s*:/g)].map((m) => m[1])); // declared in the block itself
    for (const [, name] of code[b].matchAll(/var\((--[\w-]+)\s*[,)]/g)) {
      if (code[b].includes(`var(${name},`)) continue; // an override hook with its own fallback
      assert.ok(defined.has(name) || local.has(name), `${b}: ${name} is not a token`);
    }
  }
});

test("diff-summary: the legend is a group of pressable toggles with a pressed state that is not colour alone", () => {
  const css = code["diff-summary"];
  assert.match(css, /& \.legend \{[^}]*display:\s*flex[^}]*flex-wrap:\s*wrap/s);
  assert.match(css, /button\[aria-pressed="true"\] \{[^}]*(border-color|box-shadow|outline)[^}]*\}/s);
  assert.match(css, /button\[aria-pressed="true"\] \{[^}]*font-weight:\s*var\(--weight-medium\)/s, "pressed also bolds");
  assert.match(css, /\.legend \{[\s\S]*&:focus-visible/);
  assert.match(css, /@media \(forced-colors: active\)[\s\S]*\.legend button\[aria-pressed="true"\][^}]*outline/, "forced colours outline the pressed toggle");
  assert.match(css, /@media \(pointer: coarse\)[\s\S]*\.legend button/, "touch size");
});

test("diff-summary: sections and directory groups", () => {
  const css = code["diff-summary"];
  assert.match(css, /li\.section/);
  assert.match(css, /& \.section > \.title|& li\.section > \.title|\.section > h4/);
  assert.match(css, /& \.group-head \{[^}]*min-block-size:\s*var\(--size-nav-row\)/s);
  const dirname = css.match(/& \.dirname \{[^}]*\}/s)?.[0] ?? "";
  assert.ok(dirname, "no .dirname rule");
  assert.match(dirname, /direction:\s*rtl/, "the directory truncates from its start");
  assert.match(dirname, /text-overflow:\s*ellipsis/);
  assert.match(dirname, /text-align:\s*left/);
  assert.match(css, /\.group-head \{[\s\S]*& \.count \{[^}]*margin-inline-start:\s*auto/);
  assert.match(css, /:is\(ol, li\.section, li\.group\)\s*\{[^}]*list-style:\s*none/s, "nested lists draw no bullets");
});

test("diff-summary: the staged flag and the updated dot say it in words or shape", () => {
  const css = code["diff-summary"];
  assert.match(css, /& \.flag \{[^}]*border:/s, "the flag is a bordered word");
  assert.match(css, /\[data-updated\][\s\S]*?&::before \{[^}]*border-radius:\s*var\(--radius-round\)/, "the dot is drawn before the mark");
  assert.match(css, /\[data-updated\][\s\S]*?& \.name \{[^}]*font-weight:\s*var\(--weight-medium\)/s, "an out-of-date row also bolds its name");
  assert.match(css, /@media \(forced-colors: active\)[\s\S]*\[data-updated\][^{]*::before[^}]*(CanvasText|currentColor)/, "the dot survives forced colours");
});

test("diff-view: a notice slot under the bar and the updated flag in a file head", () => {
  const css = code["diff-view"];
  assert.match(css, /& > \.notice \{[^}]*display:\s*flex[^}]*flex-wrap:\s*wrap/s);
  assert.match(css, /& > \.notice \{[^}]*border:/s, "the notice has an edge, not only a tint");
  assert.match(css, /& > \.notice\[data-state="current"\]/, "'Up to date' is quieter");
  assert.match(css, /& \.updated \{[^}]*font-size:\s*var\(--text-xs\)/s);
  assert.match(css, /@media \(forced-colors: active\)[\s\S]*\.notice/);
});

test("segmented: the count stays readable on the pressed segment", () => {
  const css = code["segmented"];
  assert.match(css, /& \.count \{/);
  assert.match(css, /\[aria-pressed="true"\][^{]*\.count[^{]*\{[^}]*color:\s*var\(--color-ink-muted\)/s);
});

test("code-source-bar: the chip slot never shrinks, the branch gives way first, then the dirty count", () => {
  const css = code["code-source-bar"];
  const slot = css.match(/& \.sync-slot \{[^}]*\}/s)?.[0] ?? "";
  assert.match(slot, /flex:\s*none/, "the chip is never cut");
  const shrink = (sel) => Number(css.match(new RegExp(`& \\.${sel} \\{[^}]*flex:\\s*0\\s+([\\d.]+)`, "s"))?.[1]);
  assert.ok(shrink("branch") > shrink("dirty") && shrink("dirty") > 0, "branch shrinks faster than dirty, dirty still shrinks");
  assert.match(css, /& \.dirty \{[^}]*min-inline-size:\s*0/s);
  assert.match(css, /& \.branch \{[^}]*min-inline-size:\s*8ch/s);
});

test("code-source-bar: a stopped session says so, with the time it is as of", () => {
  const css = code["code-source-bar"];
  assert.match(css, /&\[data-state="stopped"\]/);
  assert.match(css, /\.asof[^{]*::before\s*\{\s*content:\s*"· " \/ ""/, "a separator that readers skip");
  assert.match(css, /&\[data-state="stale"\] \{/, "stale is unchanged");
});

test("the session row: the chip wraps with the tokens, never truncates, and keeps its edge", () => {
  const row = read("./css/blocks/row.css");
  const variant = strip(row.slice(row.indexOf("/* session-variant:start */"), row.indexOf("/* session-variant:end */")));
  const chip = variant.match(/& > \.who > \.sync-state \{[^}]*\}/s)?.[0] ?? "";
  assert.ok(chip, "no .who > .sync-state rule in the session variant");
  assert.match(chip, /flex:\s*none/);
  assert.match(chip, /max-inline-size:\s*none/);
  const upstream = variant.match(/& > \.who > \.sync-state > \.upstream \{[^}]*\}/s)?.[0] ?? "";
  assert.ok(upstream && !/ellipsis|nowrap/.test(upstream) && /overflow:\s*visible/.test(upstream), "the remote name never truncates in a row");
  assert.doesNotMatch(variant, /\.sync-state[^{]*\{[^}]*border-style/, "the dashed edge of none and unknown is the chip's own");
});

for (const theme of ["light", "dark"]) {
  test(`the specimen shows every extension in ${theme}`, () => {
    assert.ok(section, "specimen has no git-changes section");
    const h = half(theme);
    assert.ok(h, `no ${theme} half`);
    for (const needle of [
      'class="legend"', 'role="group"', 'aria-pressed="true"', 'aria-pressed="false"',
      'class="section"', 'Tracked changes', 'Untracked', 'class="group"', 'class="group-head"', 'class="dirname"',
      'class="flag"', "staged", "data-updated", "new files", ">new<",
      'class="notice"', 'role="status"', 'data-state="current"', 'class="updated"',
      'class="count"', 'class="sync-slot"', 'data-state="stopped"', 'class="asof"', 'data-state="stale"',
      'data-layout="session"', "pr-badge",
    ]) assert.ok(h.includes(needle), `${theme}: ${needle}`);
    // The chip rides in a session row's .who after the pull-request badge.
    assert.match(h, /pr-badge[^>]*>[^<]*<\/a><span class="sync-state"/, `${theme}: the row's chip follows the pull-request badge`);
    for (const s of ["none", "unknown"]) assert.match(h, new RegExp(`data-layout="session"[\\s\\S]{0,1200}class="sync-state" data-state="${s}"`), `${theme}: a row with a ${s} chip`);
    // Every legend toggle carries a letter, its count and its word for readers.
    for (const [, btn] of h.matchAll(/<button[^>]*aria-pressed="(?:true|false)"[^>]*>([\s\S]*?)<\/button>/g)) {
      if (!btn.includes("change-mark")) continue;
      assert.match(btn, /<span class="count">\d+<\/span>/);
      assert.match(btn, /visually-hidden/);
    }
    // The updated dot is a shape that is also named.
    for (const m of h.matchAll(/\sdata-updated[\s>]/g)) assert.match(h.slice(m.index, m.index + 900), /title="Changed since you loaded it"/);
  });
}

test("DESIGN.md names the extensions", () => {
  for (const n of ["`.legend`", "`li.section`", "`.group-head`", "`.flag`", "`data-updated`", "`.notice`", "`.updated`", "`.sync-slot`", "`data-state=\"stopped\"`"]) {
    assert.ok(design.includes(n), n);
  }
  assert.match(design, /branch gives way first, then `\.dirty`/, "the narrow-bar priority is written");
});
