// Invariants of the memory chip block (Design: memória da sessão na UI) that text can check.
// Pixel layout is proven in a browser. Run: node --test design-system/memory-blocks.test.mjs
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const read = (p) => readFileSync(fileURLToPath(new URL(p, import.meta.url)), "utf8");
const memory = read("./css/blocks/memory.css");
const index = read("./css/index.css");
const head = read("./css/blocks/session-head.css");

test("the block is imported and lives in the blocks layer", () => {
  assert.match(index, /@import url\("\.\/blocks\/memory\.css"\);/);
  assert.match(memory, /@layer blocks \{/);
});

test("the fill moves by CSS transition, never by script, and holds still under reduced motion", () => {
  assert.match(memory, /transition: inline-size var\(--duration-settle\) var\(--ease-out\)/);
  assert.match(memory, /prefers-reduced-motion: reduce\) \{ & \.fill \{ transition: none; \}/);
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
