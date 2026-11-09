// Invariants of the interview panel's blocks (agent-stage, interview, interview-set, interview-question,
// interview-option, interview-sheet, interview-toggle) that text can check. Pixel layout is proven in a browser.
// Run: node --test design-system/interview-blocks.test.mjs
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const read = (p) => readFileSync(fileURLToPath(new URL(p, import.meta.url)), "utf8");
const strip = (css) => css.replace(/\/\*[\s\S]*?\*\//g, "");
const BLOCKS = ["agent-stage", "interview", "interview-set", "interview-history", "interview-question", "interview-note", "interview-option", "interview-sheet", "interview-toggle"];
const css = Object.fromEntries(BLOCKS.map((b) => [b, read(`./css/blocks/${b}.css`)]));
const code = Object.fromEntries(BLOCKS.map((b) => [b, strip(css[b])]));
const index = read("./css/index.css");
const tokens = read("./css/global/tokens.css");
const specimen = read("./specimen.html");
const design = read("../DESIGN.md");

test("each block is its own file under 100 lines, in @layer blocks, imported by the index", () => {
  for (const b of BLOCKS) {
    assert.match(index, new RegExp(`@import url\\("\\./blocks/${b}\\.css"\\);`), b);
    assert.match(css[b], /@layer blocks \{/, `${b} must live in @layer blocks`);
    assert.ok(css[b].split("\n").length <= 100, `${b} is over 100 lines`);
  }
});

test("tokens only: no colour literals, every var() is a token or private", () => {
  const defined = new Set([...tokens.matchAll(/(--[\w-]+)\s*:/g)].map((m) => m[1]));
  for (const b of BLOCKS) {
    assert.doesNotMatch(code[b], /#[0-9a-f]{3,8}\b|\b(rgb|rgba|hsl|hsla|oklch|oklab|lab|lch)\(/i, `${b}: a colour literal`);
    const local = new Set([...code[b].matchAll(/(--_[\w-]+)\s*:/g)].map((m) => m[1]));
    for (const [, name] of code[b].matchAll(/var\((--[\w-]+)/g)) assert.ok(defined.has(name) || local.has(name), `${b}: ${name} is not a token`);
  }
});

test("the panel docks from 56rem of session column, read from the session container, and its width follows it", () => {
  assert.match(code["agent-stage"], /@container session \(inline-size < 56rem\)\s*\{[^}]*\.interview\s*\{\s*display:\s*none/s);
  assert.match(code["interview"], /inline-size:\s*clamp\(20rem,\s*34cqi,\s*26rem\)/);
  assert.match(read("./css/blocks/session.css"), /container:\s*session\s*\/\s*inline-size/);
});

test("the panel's width does not animate; only its content fades, and not under reduced motion", () => {
  assert.doesNotMatch(code["interview"], /transition[^;]*inline-size|transition:\s*all/);
  assert.match(code["interview"], /animation:\s*interview-in var\(--duration-quick\)/);
  assert.match(code["interview"], /prefers-reduced-motion: reduce\)\s*\{[^}]*animation:\s*none/s);
});

test("the panel scrolls inside itself and the terminal may shrink to make room", () => {
  assert.match(code["interview"], /overscroll-behavior:\s*contain/);
  assert.match(code["agent-stage"], /\.terminal\s*\{\s*min-inline-size:\s*0/);
});

test("every animation is switched off under reduced motion", () => {
  for (const b of BLOCKS) {
    if (!/animation\s*:/.test(code[b])) continue;
    assert.match(code[b], /@media \(prefers-reduced-motion: reduce\)/, `${b} animates without a reduced-motion rule`);
  }
});

test("the recommendation and the focused question are borders, so they survive forced colours", () => {
  assert.match(code["interview-option"], /\[data-recommended\]\s*\{[^}]*border-inline-start:\s*3px solid var\(--color-signal\)/s);
  assert.match(code["interview-question"], /:focus-within\s*\{\s*border-inline-start-color:\s*var\(--color-attention\)/);
  assert.match(code["interview-question"], /forced-colors: active/);
});

test("the sheet is out of flow, centred to 40rem and 88% high, full width and 92% on a phone", () => {
  const sheet = code["interview-sheet"];
  assert.match(sheet, /\.interview-sheet\s*\{[^}]*position:\s*absolute/s);
  assert.match(sheet, /inline-size:\s*min\(100%,\s*40rem\)/);
  assert.match(sheet, /max-block-size:\s*88%/);
  assert.match(sheet, /@media \(width < 48rem\)\s*\{[^}]*max-block-size:\s*92%/s);
  assert.match(sheet, /\.interview-peek\s*\{[^}]*position:\s*absolute/s);
});

test("the footer sticks to the foot of the panel's scroller and the progress bar is a token-coloured fill", () => {
  assert.match(code["interview-set"], /> footer\s*\{[^}]*position:\s*sticky;[^}]*inset-block-end:\s*0/s);
  assert.match(code["interview-set"], /\.bar > i\s*\{[^}]*background:\s*var\(--color-signal\)/s);
});

test("the note is a quiet disclosure: its marker is drawn, and its summary shows a focus ring", () => {
  assert.match(code["interview-note"], /::before\s*\{\s*content:\s*"\+"/);
  assert.match(code["interview-note"], /summary:focus-visible|&:focus-visible\s*\{[^}]*outline/s);
});

test("a folded or closed interview is a row: a full-width button with a chevron that turns, on the hover wash", () => {
  const set = code["interview-history"];
  assert.match(set, /> \.head\s*\{[^}]*display:\s*flex[^}]*inline-size:\s*100%/s);
  assert.match(set, /> button\.head:hover\s*\{[^}]*background:\s*var\(--color-hover\)/s);
  assert.match(set, /\.chev\s*\{[^}]*rotate:\s*45deg/s);
  assert.match(set, /\[data-collapsed\]\s*>\s*\.head\s*\.chev\s*\{[^}]*rotate:\s*-45deg/s);
  assert.match(set, /\.head:focus-visible\s*\{[^}]*outline/s);
});

test("a closed interview reads back its answers: Skipped is italic and faint, a note has a rule beside it", () => {
  const history = code["interview-history"];
  assert.match(history, /\.skipped\s*\{[^}]*font-style:\s*italic[^}]*color:\s*var\(--color-ink-faint\)|\.skipped\s*\{[^}]*color:\s*var\(--color-ink-faint\)[^}]*font-style:\s*italic/s);
  assert.match(history, /\.note\s*\{[^}]*border-inline-start:\s*2px solid var\(--color-line-strong\)/s);
  assert.match(history, /\.interview-group\s*\{[^}]*text-transform:\s*uppercase/s);
});

test("the specimen and DESIGN.md show each block", () => {
  for (const b of BLOCKS.filter((b) => b !== "agent-stage")) {
    assert.ok(specimen.includes(`${b} `) || specimen.includes(`${b}\``) || specimen.includes(`"[ ${b} `) || specimen.includes(`[ ${b} ]`), `${b} is not in the specimen`);
    assert.ok(design.includes(`.${b}`), `${b} is not in DESIGN.md`);
  }
  assert.match(specimen, /class="\[ agent-stage \]"/);
});
