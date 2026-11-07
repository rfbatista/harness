// Invariants of the services blocks (service row, service log, port and number form rows, split-view
// variants, services icon) that text can check. Pixel layout is proven in a browser, not here.
// Run: node --test design-system/services-blocks.test.mjs
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const read = (p) => readFileSync(fileURLToPath(new URL(p, import.meta.url)), "utf8");
const strip = (css) => css.replace(/\/\*[\s\S]*?\*\//g, "");
const service = strip(read("./css/blocks/service.css"));
const splitView = strip(read("./css/blocks/split-view.css"));
const mcpForm = strip(read("./css/blocks/mcp-form.css"));
const index = read("./css/index.css");
const tokens = read("./css/global/tokens.css");
const icons = read("../internal/adapter/in/web/shell/icons.templ");

test("service.css lives in the blocks layer and is imported by the index", () => {
  assert.match(service, /@layer blocks \{/);
  assert.match(index, /@import url\("\.\/blocks\/service\.css"\);/);
});

test("the service row and the log viewer exist", () => {
  assert.match(service, /\.row\[data-layout="service"\]/);
  assert.match(service, /\.service-log\b/);
  assert.match(service, /\.ln\[data-harness\]/);
  assert.match(service, /\.reset\b/);
  assert.match(service, /@container services \(width < 46rem\)/);
});

test("tokens only: no colour literal, every var() is a token", () => {
  const defined = new Set([...tokens.matchAll(/(--[\w-]+)\s*:/g)].map((m) => m[1]));
  const local = new Set([...service.matchAll(/(--[\w-]+)\s*:/g)].map((m) => m[1]));
  assert.doesNotMatch(service, /#[0-9a-f]{3,8}\b|\b(rgb|rgba|hsl|hsla|oklch)\(/i);
  for (const [, name] of service.matchAll(/var\((--[\w-]+)/g)) {
    assert.ok(defined.has(name) || local.has(name), `${name} is not a token`);
  }
});

test("a notice slot over the list has its own padding, from tokens", () => {
  assert.match(service, /\.list\[data-services\] > \.slot\s*\{[^}]*padding: var\(--space-s\) var\(--space-l\) 0/);
});

test("harness lines differ from program output by more than colour", () => {
  assert.match(service, /\.ln\[data-harness\][^{]*\{[^}]*box-shadow: inset/);
  assert.match(service, /\.ln\[data-harness\] > b\s*\{[^}]*font-weight/);
});

test("split-view: a list alone, and a wide list collapsing below 64rem", () => {
  assert.match(splitView, /&\[data-no-detail\]\s*\{\s*grid-template-columns: minmax\(0, 1fr\);/);
  assert.match(splitView, /&\[data-wide-list\]:not\(\[data-detail\]\)|data-wide-list[^}]*minmax\(0, 1fr\)/);
});

test("form rows for ports and numbers", () => {
  assert.match(mcpForm, /&\[data-kind="port"\]\s*\{[^}]*grid-template-columns/);
  assert.match(mcpForm, /&\[data-kind="num"\]\s*\{[^}]*grid-template-columns/);
});

test("the services icon is drawn at 16px beside the others", () => {
  assert.match(icons, /case "services":\s*<svg aria-hidden="true" viewBox="0 0 16 16">/);
});
