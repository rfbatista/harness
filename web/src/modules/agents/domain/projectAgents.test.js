import { assert, file, test } from "../../../shared/testing/test.js";
import { catalogDTO } from "../testing/fixtures.js";
import { toCatalog } from "../infrastructure/dto.js";
import {
  agentPicker, agentRows, agentsChanged, agentsCount, agentsPhrase, agentsSaved, agentsToWrite, emptySelection, removedAgents, startSelected,
} from "./projectAgents.js";

file("agents/domain/projectAgents");

const catalog = toCatalog(catalogDTO());
const many = (n) => ({ agents: Array.from({ length: n }, (_, i) => ({ name: `a${String(i).padStart(2, "0")}`, description: i % 2 ? "" : `Agent ${i}` })) });

test("a row per catalog agent, with its description or No description", () => {
  assert.deepEqual(agentRows(catalog, ["backend"]).map((r) => [r.name, r.description, r.checked]), [["ux-designer", "Designs screens.", false], ["backend", "No description", true]]);
});

test("the saved list is the catalog's agents in catalog order; All saves no list", () => {
  assert.deepEqual(agentsSaved(catalog, { mode: "selected", agents: ["backend", "ghost", "ux-designer"] }), { mode: "selected", agents: ["ux-designer", "backend"] });
  assert.deepEqual(agentsSaved(catalog, { mode: "all", agents: ["backend"] }), { mode: "all", agents: [] });
});

test("switching to Selected ticks every agent unless a list was kept, even an empty one", () => {
  assert.deepEqual(startSelected(catalog, null), ["ux-designer", "backend"]);
  assert.deepEqual(startSelected(catalog, ["backend"]), ["backend"]);
  assert.deepEqual(startSelected(catalog, []), []);
});

test("the list is ignored while All is on: only the mode and, in Selected, the set differ", () => {
  const all = { mode: "all", agents: [] };
  const sel = { mode: "selected", agents: ["backend", "ux-designer"] };
  assert.equal(agentsChanged(all, "all", ["backend"]), false, "a kept list does not make All dirty");
  assert.equal(agentsChanged(all, "selected", ["backend"]), true);
  assert.equal(agentsChanged(sel, "all", ["backend"]), true);
  assert.equal(agentsChanged(sel, "selected", ["ux-designer", "backend"]), false, "order does not matter");
  assert.equal(agentsChanged(sel, "selected", ["backend"]), true);
});

test("what is written: All sends an empty list, Selected only catalog agents", () => {
  assert.deepEqual(agentsToWrite(catalog, "all", ["backend"]), { mode: "all", agents: [] });
  assert.deepEqual(agentsToWrite(catalog, "selected", ["ghost", "backend"]), { mode: "selected", agents: ["backend"] });
  assert.deepEqual(agentsToWrite(catalog, "selected", []), { mode: "selected", agents: [] });
});

test("the empty-list warning shows only in Selected with nothing ticked", () => {
  assert.equal(emptySelection("selected", []), true);
  assert.equal(emptySelection("selected", ["backend"]), false);
  assert.equal(emptySelection("all", []), false);
});

test("the count and the toast phrase", () => {
  assert.equal(agentsCount("selected", ["a"], 4), "1 of 4 enabled");
  assert.equal(agentsCount("all", null, 4), "All 4 enabled");
  assert.equal(agentsCount("all", null, 0), "");
  assert.equal(agentsPhrase({ mode: "all", agents: [] }), "enables all agents");
  assert.equal(agentsPhrase({ mode: "selected", agents: ["a"] }), "enables 1 agent");
});

test("an agent the catalog lost is named; a rename names none", () => {
  const before = { mode: "selected", agents: ["backend", "old"] };
  assert.deepEqual(removedAgents(before, catalog, { mode: "selected", agents: ["backend"] }), ["old"]);
  assert.deepEqual(removedAgents(before, catalog, { mode: "selected", agents: ["backend", "renamed"] }), []);
  assert.deepEqual(removedAgents({ mode: "all", agents: [] }, catalog), []);
});

test("the combobox offers unticked agents matching the query and chips the ticked", () => {
  const c = many(20);
  const p = agentPicker(c, ["a01"], "agent 1");
  assert.deepEqual(p.options.map((o) => o.key), ["a10", "a12", "a14", "a16", "a18"].filter((k) => c.agents.find((a) => a.name === k).description.toLowerCase().includes("agent 1")));
  assert.deepEqual(p.chips, ["a01"]);
  assert.equal(agentPicker(c, [], "zzz").empty, true);
  assert.equal(agentPicker(c, [], "").options.length, 20);
});
