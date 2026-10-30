// Invariants of the sync-state, sync-panel and git-popover blocks (contract: design system <-> web UI,
// code views, revision 6) that text can check. Pixel layout is proven in a browser, not here.
// Run: node --test design-system/sync-blocks.test.mjs
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const read = (p) => readFileSync(fileURLToPath(new URL(p, import.meta.url)), "utf8");
const strip = (css) => css.replace(/\/\*[\s\S]*?\*\//g, "");
const BLOCKS = ["sync-state", "sync-panel", "git-popover"];
const css = Object.fromEntries(BLOCKS.map((b) => [b, read(`./css/blocks/${b}.css`)]));
const code = Object.fromEntries(BLOCKS.map((b) => [b, strip(css[b])]));
const index = read("./css/index.css");
const tokens = read("./css/global/tokens.css");
const specimen = read("./specimen.html");
const design = read("../DESIGN.md");

const STATES = ["in-sync", "ahead", "behind", "diverged", "none", "gone", "unknown"];

test("each block is its own file, imported inside the index after code-source-bar", () => {
  for (const b of BLOCKS) {
    assert.match(index, new RegExp(`@import url\\("\\./blocks/${b}\\.css"\\);`), b);
    assert.match(css[b], /@layer blocks \{/, `${b} must live in @layer blocks`);
  }
});

test("tokens only: no colour, spacing or radius literals, every var() is a token or private", () => {
  const defined = new Set([...tokens.matchAll(/(--[\w-]+)\s*:/g)].map((m) => m[1]));
  for (const b of BLOCKS) {
    assert.doesNotMatch(code[b], /#[0-9a-f]{3,8}\b|\b(rgb|rgba|hsl|hsla|oklch|oklab|lab|lch)\(/i, `${b}: a colour literal`);
    const local = new Set([...code[b].matchAll(/(--_[\w-]+)\s*:/g)].map((m) => m[1]));
    for (const [, name] of code[b].matchAll(/var\((--[\w-]+)/g)) {
      assert.ok(defined.has(name) || local.has(name), `${b}: ${name} is not a token`);
    }
    for (const [, name] of code[b].matchAll(/^\s*(--[\w-]+)\s*:/gm)) assert.match(name, /^--_/, `${b}: ${name} must be private`);
    // Lengths come from tokens; the only literals allowed are the chip's and the panels' rem widths and 0/1 values.
    for (const [, decl] of code[b].matchAll(/(?:^|[;{\s])(padding[\w-]*|margin[\w-]*|gap|border-radius|font-size)\s*:\s*([^;}]+)/g).map((m) => [0, `${m[1]}: ${m[2]}`])) {
      assert.doesNotMatch(decl, /\d+(\.\d+)?(px|rem|em)\b/, `${b}: ${decl} must use a token`);
    }
  }
});

test("forced colours: the chip draws its edge in CanvasText", () => {
  assert.match(code["sync-state"], /@media \(forced-colors: active\)[^}]*\{[^}]*CanvasText/s);
});

test("reduced motion: every animation is switched off", () => {
  for (const b of BLOCKS) {
    if (!/animation\s*:/.test(code[b])) continue;
    assert.match(code[b], /@media \(prefers-reduced-motion: reduce\)/, `${b} animates without a reduced-motion rule`);
  }
  assert.match(code["sync-state"], /animation:/, "the chip's spinner is the one thing that spins");
});

test("no state by colour alone: every state has a word, a glyph or a dashed edge", () => {
  const chip = code["sync-state"];
  assert.match(chip, /\[data-state="in-sync"\][^{]*\.word::before\s*\{\s*content:\s*"✓ " \/ ""/, "in sync draws a check");
  assert.match(chip, /\[data-state="none"\][^{]*\[data-state="unknown"\][^{]*\{[^}]*border-style:\s*dashed|\[data-state="unknown"\][^{]*\[data-state="none"\][^{]*\{[^}]*border-style:\s*dashed/);
  assert.match(chip, /\[data-loading\][^{]*\.word::before/, "loading draws a ring before the word");
  // The attention tone of behind/diverged/gone never travels alone: the specimen gives each a word or counts.
  const starts = [...specimen.matchAll(/<(?:span|button)[^>]*class="sync-state"/g)].map((m) => m.index);
  assert.ok(starts.length >= STATES.length * 2, "specimen shows the chip in light and dark");
  for (const i of starts) {
    const html = specimen.slice(i, i + 700);
    assert.match(html, /class="word"|class="counts"/, `a chip without a word or counts: ${html.slice(0, 120)}`);
  }
  for (const [, tag] of specimen.matchAll(/<span([^>]*)>[\u2191\u2193]<\/span>/g)) assert.match(tag, /aria-hidden="true"/);
  for (const m of specimen.matchAll(/<span class="(?:ahead|behind)">/g)) {
    assert.match(specimen.slice(m.index, m.index + 160), /visually-hidden/, "the word ahead/behind is there for readers");
  }
});

test("the chip, the panel and the popover keep the type scale: line heights stay on the tokens", () => {
  for (const b of BLOCKS) {
    for (const [, value] of code[b].matchAll(/line-height\s*:\s*([^;}]+)/g)) {
      assert.match(value.trim(), /^(var\(--leading-(tight|snug|normal)\)|1|inherit)$/, `${b}: line-height ${value}`);
    }
  }
  const tok = JSON.parse(read("./tokens.json"));
  assert.deepEqual(tok.leading, { tight: "1.25", snug: "1.4", normal: "1.55" });
  assert.match(tokens, /--leading-tight:\s*1\.25/);
});

test("the chip names every state of the contract", () => {
  for (const s of STATES) assert.match(code["sync-state"], new RegExp(`data-state="${s}"`), s);
  assert.match(code["sync-state"], /\[data-loading\]/);
  assert.match(code["sync-state"], /\[data-old\]/);
  assert.match(code["sync-state"], /\.upstream[^{]*\{[^}]*text-overflow:\s*ellipsis/s, "the remote name truncates");
});

test("the panels are 24rem and 36rem and become bottom sheets below 48rem", () => {
  assert.match(code["sync-panel"], /--_width:\s*24rem/);
  assert.match(code["git-popover"], /--_width:\s*36rem/);
  for (const b of ["sync-panel", "git-popover"]) {
    assert.match(code[b], /&\[data-sheet\]\s*\{[^}]*inline-size:\s*100%/s, `${b} data-sheet is full width`);
    assert.match(css[b], /class="popup /, `${b} rides the popup block`);
  }
  assert.match(code["sync-panel"], /\.fresh\[data-old\]|&\[data-old\]/);
  assert.match(code["sync-panel"], /\[aria-busy="true"\]/);
  assert.match(code["git-popover"], /\.pop-foot|footer|\.foot/);
});

for (const theme of ["light", "dark"]) {
  test(`the specimen shows every chip state, the panel and the popover in ${theme}`, () => {
    const start = specimen.indexOf('id="git-sync"');
    assert.ok(start > 0, "specimen has no git-sync section");
    const section = specimen.slice(start, specimen.indexOf("</section>", start));
    const half = section.split(/(?=<div[^>]*specimen-theme)/).find((h) => h.includes(`data-theme="${theme}"`)) ?? "";
    assert.ok(half, `no ${theme} half`);
    for (const s of STATES) assert.match(half, new RegExp(`class="sync-state" data-state="${s}"`), `${theme}: ${s}`);
    for (const needle of ["data-loading", "data-old", "sync-panel", "git-popover", "data-fetch", "data-push", "confirm-strip", 'aria-busy="true"', 'data-tone="danger"', "banner", "data-sheet"]) {
      assert.ok(half.includes(needle), `${theme}: ${needle}`);
    }
  });
}

test("DESIGN.md names the three blocks and their parts", () => {
  for (const n of ["`.sync-state`", "`.sync-panel`", "`.git-popover`", "`data-loading`", "`data-old`", "data-fetch]", "data-push]"]) {
    assert.ok(design.includes(n), n);
  }
  for (const s of STATES) assert.ok(design.includes(`\`${s}\``), s);
});
