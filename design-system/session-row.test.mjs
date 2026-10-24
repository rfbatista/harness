// Invariants of the session row variant (ADR 0029) that text can check. Pixel
// layout is proven in a browser, not here. Run: node --test design-system/
import { test } from "node:test";
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const read = (p) => readFileSync(fileURLToPath(new URL(p, import.meta.url)), "utf8");
const row = read("./css/blocks/row.css");
const status = read("./css/blocks/status.css");
const design = read("../DESIGN.md");

const START = "/* session-variant:start */";
const END = "/* session-variant:end */";
const variantStart = row.indexOf(START);
const variantEnd = row.indexOf(END);
const variant = variantStart >= 0 && variantEnd > variantStart ? row.slice(variantStart, variantEnd) : "";

// sha256 of row.css from `@layer blocks` on, as it was before the variant.
const ROW_BEFORE = "5f3b6b2f44e02efea3c056074fe32a68d72349cee1af121cc280cfdc9a60fe45";

test("the variant sits between its markers inside @layer blocks", () => {
  assert.ok(variant, "row.css has no session-variant markers");
  assert.ok(row.indexOf("@layer blocks") < variantStart, "variant must be inside @layer blocks");
  assert.match(variant, /\.row\[data-layout="session"\]/);
});

test("every selector in the variant is scoped under the session layout", () => {
  // The variant is one rule with nested selectors; its only top-level selector is the scoped one.
  const topLevel = [...variant.replace(/\/\*[\s\S]*?\*\//g, "").matchAll(/^ {2}([^\s}][^{]*)\{/gm)].map((m) => m[1].trim());
  assert.deepEqual(topLevel, ['.row[data-layout="session"]']);
});

test("the existing .row rules are byte-identical", () => {
  const body = row.slice(row.indexOf("@layer blocks"));
  const without = body.replace(/\n *\/\* session-variant:start \*\/[\s\S]*?\/\* session-variant:end \*\/\n?/, "\n");
  const clean = without.replace(/\n{2,}(?=  @keyframes)/, "\n\n");
  const digest = (s) => createHash("sha256").update(s).digest("hex");
  assert.equal(digest(clean), ROW_BEFORE);
  for (const kept of ["&[data-stacked]", "& .meta", "&[data-depth]"]) assert.ok(clean.includes(kept), kept);
});

test("the variant adds no custom properties of its own to the token set", () => {
  const defined = new Set([...read("./css/global/tokens.css").matchAll(/(--[\w-]+)\s*:/g)].map((m) => m[1]));
  const used = [...variant.matchAll(/var\((--[\w-]+)/g)].map((m) => m[1]);
  const local = new Set([...variant.matchAll(/(--_[\w-]+)\s*:/g)].map((m) => m[1]));
  for (const name of used) assert.ok(defined.has(name) || local.has(name), `${name} is not a token`);
  const declared = [...variant.matchAll(/^\s*(--[\w-]+)\s*:/gm)].map((m) => m[1]);
  for (const name of declared) assert.match(name, /^--_/, `${name} must be private (--_)`);
});

test("the detail clamps to two lines and line 1 does not truncate", () => {
  assert.match(variant, /-webkit-line-clamp:\s*2/);
  assert.match(variant, /(?<!-webkit-)line-clamp:\s*2/);
  const who = variant.match(/& > \.who \{[^}]*\}/)?.[0] ?? "";
  assert.ok(who && !/nowrap|ellipsis/.test(who), ".who must wrap");
  const agent = variant.match(/& \.agent \{[^}]*\}/)?.[0] ?? "";
  assert.ok(agent && !/nowrap|ellipsis/.test(agent), ".agent must wrap");
});

test("a check with no lead or quote has no separator", () => {
  assert.match(variant, /\.check/);
  assert.match(variant, /\.check:not\(:first-child\)::before|\.check:first-child::before\s*\{\s*content:\s*none/);
});

test("data-dot-only reserves no width", () => {
  const rule = status.match(/&\[data-dot-only\]\s*\{([^}]*)\}/)?.[1] ?? "";
  assert.match(rule, /gap:\s*0/);
  assert.doesNotMatch(rule, /inline-size|width|padding|margin/);
  assert.doesNotMatch(variant, /row-status-width/);
});

test("DESIGN.md records the session-row exception to the Word Beside the Dot Rule", () => {
  const rule = design.split("\n").find((l) => l.includes("The Word Beside the Dot Rule")) ?? "";
  assert.match(design.slice(design.indexOf("The Word Beside the Dot Rule")).split("\n\n")[0], /session rows?/i);
  assert.match(design.slice(design.indexOf("The Word Beside the Dot Rule")).split("\n\n")[0], /ADR 0029/);
  assert.ok(rule);
});
