import views from "../../../../testdata/views/agents-routes.json" with { type: "json" };
import { assert, file, test } from "../../../shared/testing/test.js";
import { TABS, routeOf, tabAfterKey } from "./tabs.js";

file("agents/domain/tabs");

test("the page has five tabs, and every tab but Tools has a New button naming it", () => {
  assert.deepEqual(TABS.map((t) => t.id), ["agents", "skills", "bundles", "mcp", "tools"]);
  assert.deepEqual(TABS.map((t) => t.newLabel), ["New agent", "New skill", "New bundle", "New MCP server", ""]);
  assert.deepEqual(TABS.map((t) => t.newHref), ["/agents/new", "/agents/skills/new", "/agents/bundles/new", "/agents/mcp/new", ""]);
});

test("each route selects its tab, with the item and the editor it names (the Go server reads the same table)", () => {
  for (const r of views.routes) assert.deepEqual(routeOf(r.path), { tab: r.tab, item: r.item, mode: r.mode }, r.path);
});

test("a path outside the Agents page is no route", () => {
  for (const path of views.none) assert.equal(routeOf(path), null, path);
});

test("arrow keys move between tabs and wrap; Home and End go to the ends; other keys do nothing", () => {
  assert.equal(tabAfterKey("agents", "ArrowRight"), "skills");
  assert.equal(tabAfterKey("tools", "ArrowRight"), "agents");
  assert.equal(tabAfterKey("agents", "ArrowLeft"), "tools");
  assert.equal(tabAfterKey("mcp", "ArrowLeft"), "bundles");
  assert.equal(tabAfterKey("bundles", "Home"), "agents");
  assert.equal(tabAfterKey("bundles", "End"), "tools");
  assert.equal(tabAfterKey("bundles", "Enter"), null);
  assert.equal(tabAfterKey("bundles", "a"), null);
});
