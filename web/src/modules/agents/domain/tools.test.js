import { assert, file, test } from "../../../shared/testing/test.js";
import { toCatalog } from "../infrastructure/dto.js";
import { catalogDTO } from "../testing/fixtures.js";
import { toolDetail, toolRows, toolsFooter } from "./tools.js";

file("agents/domain/tools");

test("each built-in tool lists the agents whose allowed tools name it", () => {
  const c = toCatalog(catalogDTO());
  const rows = toolRows(c);
  assert.deepEqual(rows.map((r) => [r.name, r.meta]), [["Read", "1 agent"], ["Bash", "no agent"]]);
  assert.deepEqual(toolDetail(c, "Read").allowedBy, [{ name: "ux-designer", href: "/agents/ux-designer" }]);
  assert.equal(toolDetail(c, "Bash").unused, true);
  assert.equal(toolDetail(c, "Nope"), null);
});

test("the Unused quick filter keeps the tools no agent allows; the text filter reads name and description", () => {
  const c = toCatalog(catalogDTO());
  assert.deepEqual(toolRows(c, { filter: "unused" }).map((r) => r.name), ["Bash"]);
  assert.deepEqual(toolRows(c, { query: "reads a" }).map((r) => r.name), ["Read"]);
  assert.equal(toolsFooter(c), "Claude Code built-in tools · read-only");
});
