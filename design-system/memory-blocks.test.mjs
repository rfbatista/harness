// Invariants of the memory chip block (Design: memória da sessão na UI) that text can check.
// Pixel layout is proven in a browser. Run: node --test design-system/memory-blocks.test.mjs
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const read = (p) => readFileSync(fileURLToPath(new URL(p, import.meta.url)), "utf8");
const memory = read("./css/blocks/memory.css");
const index = read("./css/index.css");
const toast = read("./css/blocks/toast.css");
const board = read("./css/blocks/whiteboard.css");
const head = read("./css/blocks/session-head.css");

test("the block is imported and lives in the blocks layer", () => {
  assert.match(index, /@import url\("\.\/blocks\/memory\.css"\);/);
  assert.match(memory, /@layer blocks \{/);
});

test("the fill moves by CSS transition, never by script, and holds still under reduced motion", () => {
  assert.match(memory, /transition: inline-size var\(--duration-settle\) var\(--ease-out\)/);
  assert.match(memory, /prefers-reduced-motion: reduce\) \{\s*& \.fill \{ transition: none; \}/);
});

test("every state has a tone token and the word is in the markup, not only the colour", () => {
  for (const state of ["near", "over", "ended"]) assert.match(memory, new RegExp(`\\[data-state="${state}"\\]`));
  assert.doesNotMatch(memory, /#[0-9a-f]{3,6}\b|oklch\(/i, "colours come from tokens");
});

test("normal hides the chip with the token total in a narrow column; the other states stay", () => {
  assert.match(memory, /@container session \(width < 34rem\) \{ &\[data-state="normal"\] \{ display: none; \} \}/);
});

test("the head gives the island wrappers no box of their own", () => {
  assert.match(head, /\[data-memory-chip\] > \[data-island\] \{ display: contents; \}/);
});

test("the toast's danger border mixes with transparent, not with another colour (an oklch mix of two hues came out olive)", () => {
  assert.match(toast, /\[data-tone="danger"\] \{\s*border-color: color-mix\(in oklch, var\(--color-danger\) 45%, transparent\);/);
  assert.doesNotMatch(toast, /var\(--color-danger\)[^;]*var\(--color-line-strong\)\)/);
});

test("the toast block keeps its coloured border and its animation; the memory toasts only add their body", () => {
  assert.match(toast, /border: var\(--border-width\) solid var\(--color-line-strong\)/);
  assert.match(toast, /animation: toast-in /);
  assert.doesNotMatch(memory, /\.toast \{[^}]*border-color/);
});

test("the ring turns once and is a static outline under reduced motion", () => {
  assert.match(memory, /\[data-signal\] > \.track \{ animation: memory-signal var\(--duration-signal\) var\(--ease-out\) 1; \}/);
  assert.match(memory, /animation: none; outline: 2px solid/);
});

test("a card's memory line has one form (a fixed 3rem track), takes the last grid row, and the compact card keeps only a 2px track", () => {
  assert.match(board, /\.wb-node > \.mem \{[^}]*grid-area: mem/s);
  assert.match(board, /& > \.track \{ flex: none; inline-size: 3rem; block-size: 2px;/);
  assert.match(board, /\.wb-world\[data-compact\] \.wb-node > \.mem \{ position: absolute; inset: auto 0 0 0;/);
  assert.match(board, /\.wb-world\[data-compact\] \.wb-node \.who \.badge\[data-mem\] \{ display: inline-flex; \}/);
  assert.match(board, /\.wb-node \.who \.badge\[data-mem\] \{ display: none; \}/, "the badge is for the compact card only");
});

test("an ended process rings the card once in danger, holds still under reduced motion, and never takes the attention wash", () => {
  assert.match(board, /\.wb-node\[data-ended-process\] \{ animation: wb-ring-danger 2\.4s var\(--ease-out\) 1; \}/);
  assert.match(board, /\.wb-node\[data-ended-process\] \{ animation: none; outline: 2px solid/);
  assert.doesNotMatch(board, /data-ended-process\][^{]*\{[^}]*attention-wash/);
});

test("the chip is the last meta item before the actions, so showing it later moves nothing in the head", () => {
  assert.match(head, /& > \.meta \.memory \{ order: 1; \}/);
  assert.match(head, /& > \.actions \{ order: 2; \}/);
});

test("in the head only the chip's word gives way: the chip never shrinks below its value, the word takes an ellipsis, the token total yields below 50rem, and the actions stay whole", () => {
  assert.match(memory, /flex: 0 1 auto;\s*min-inline-size: min-content;/);
  assert.match(memory, /& > \.word \{ min-inline-size: 0; overflow: hidden; text-overflow: ellipsis;/);
  assert.match(head, /& > \.actions \{[^}]*flex: none;/);
  assert.match(head, /@container session \(width < 50rem\) \{ & > \.meta:has\(\.memory:not\(\[hidden\]\)\) > \.usage-total \{ display: none; \} \}/);
});
