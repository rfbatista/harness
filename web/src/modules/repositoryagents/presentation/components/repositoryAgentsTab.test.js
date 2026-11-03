import { flush } from "../../../../shared/testing/doubles.js";
import { mount } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { memoryRepositoryAgents } from "../../infrastructure/memory-gateway.js";
import { makeWorld } from "../../testing/fixtures.js";
import { repositoryAgentsTab } from "./repositoryAgentsTab.js";

file("repositoryagents/presentation/components/repositoryAgentsTab");

async function open(world = makeWorld()) {
  const memory = memoryRepositoryAgents(world);
  const el = document.createElement("section");
  Object.assign(el.dataset, { projectId: "p1", repositoryId: "r1", layersHref: "/projects/p1/repositories/r1/architecture/layers" });
  const m = mount(repositoryAgentsTab({ gateway: memory.gateway }), { el });
  await m.instance.init();
  return { ...m, tab: m.instance, memory, gateway: memory.gateway };
}
const names = (rows) => rows.map((r) => r.name);

test("the tab groups agents by state and counts the assigned ones", async () => {
  const { tab } = await open();
  assert.deepEqual(names(tab.assignedRows), ["reviewer", "tester"]);
  assert.deepEqual(names(tab.notAssignedRows), ["backend"]);
  assert.deepEqual(names(tab.notEnabledRows), ["docs"]);
  assert.equal(tab.stats.assigned.value, "2 of 3");
  assert.equal(tab.notAssignedTitle, "Not assigned to atlas-api (1)");
  assert.equal(tab.notEnabledTitle, "Not enabled in atlas (1)");
  const reviewer = tab.assignedRows[0];
  assert.equal(reviewer.promptAction, "Edit prompt");
  assert.equal(reviewer.summary, "“Run make check before every commit.”");
  assert.deepEqual(reviewer.notes, ["was “rev”"]);
  assert.equal(tab.assignedRows[1].summary, "No repository prompt");
});

test("every badge carries a word, and a not enabled row says why its button is disabled", async () => {
  const { tab } = await open();
  for (const r of [...tab.assignedRows, ...tab.notAssignedRows, ...tab.notEnabledRows]) assert.ok(r.badges.every((b) => b.word.length > 0));
  const docs = tab.notEnabledRows[0];
  assert.equal(docs.assignDisabled, true);
  assert.equal(docs.assignDisabledReason, "Enable docs in atlas first");
  assert.ok(docs.badges.some((b) => b.word === "Repository prompt kept") === false, "docs has no prompt");
  assert.ok(docs.notes[0].includes("stays unassigned here"));
  assert.equal(docs.enableHref, "/projects/p1/settings#settings-agents-enabled");
  assert.ok(tab.notAssignedRows[0].badges.some((b) => b.word === "Repository prompt kept"));
});

test("Unassign acts at once, moves the row, and the toast names the running sessions and offers Undo", async () => {
  const { tab } = await open();
  await tab.unassign("reviewer");
  assert.deepEqual(names(tab.notAssignedRows), ["reviewer", "backend"]);
  assert.equal(tab.stats.assigned.value, "1 of 3");
  assert.equal(tab.toast.text, "Unassigned reviewer from atlas-api. 1 running session continues. It stays available in other repositories.");
  await tab.undo();
  assert.deepEqual(names(tab.assignedRows), ["reviewer", "tester"]);
  assert.equal(tab.toast.text, "reviewer is assigned to atlas-api again. Its repository prompt is back in use.");
});

test("the running-session clause is dropped when none runs", async () => {
  const { tab } = await open();
  await tab.unassign("tester");
  assert.equal(tab.toast.text, "Unassigned tester from atlas-api. It stays available in other repositories.");
});

test("a stale click is refused: the list is the current one, the banner names the change, nothing is applied", async () => {
  const { tab, gateway } = await open();
  const seen = await gateway.getRepositoryAgents("r1");
  await gateway.unassignAgent({ repositoryId: "r1", agent: "tester", listVersion: seen.listVersion });
  await tab.unassign("reviewer");
  assert.deepEqual(names(tab.assignedRows), ["reviewer"], "reviewer is still assigned, tester is gone from the assigned group");
  assert.ok(tab.stale.text.includes("tester is now unassigned"), tab.stale.text);
  assert.equal(tab.toast, null);
  await tab.unassign("reviewer");
  assert.equal(tab.stale, null, "the next click is made knowingly");
  assert.deepEqual(names(tab.assignedRows), []);
});

test("nothing assigned: the attention banner offers Assign all again", async () => {
  const { tab } = await open();
  await tab.unassign("reviewer");
  assert.equal(tab.nothingAssigned, null);
  await tab.unassign("tester");
  assert.ok(tab.nothingAssigned.text.startsWith("No agent can run in atlas-api. You unassigned all 3 enabled agents."));
  await tab.assignAll();
  assert.equal(tab.nothingAssigned, null);
  assert.deepEqual(names(tab.assignedRows), ["reviewer", "tester", "backend"]);
});

test("the project's mode explains itself: Selected shows a banner, All a line and no banner", async () => {
  const { tab } = await open();
  assert.ok(tab.notice.banner.text.startsWith("atlas is in Selected agents mode: 3 of 5 catalog agents are enabled."));
  const all = await open(makeWorld({ projects: [{ id: "p1", name: "atlas", agentsMode: "all" }], unassigned: {} }));
  assert.equal(all.tab.notice.banner, null);
  assert.equal(all.tab.notice.line, "All agents are enabled in atlas. Unassigning only affects atlas-api.");
  assert.deepEqual(names(all.tab.notEnabledRows), []);
  const none = await open(makeWorld({ projects: [{ id: "p1", name: "atlas", agentsMode: "selected", enabled: [] }], unassigned: {} }));
  assert.ok(none.tab.notice.banner.text.includes("enables no agent"));
});

test("a change another person made arrives through the feed; one for another repository does not", async () => {
  const { tab, gateway } = await open();
  const seen = await gateway.getRepositoryAgents("r1");
  await gateway.unassignAgent({ repositoryId: "r1", agent: "tester", listVersion: seen.listVersion });
  await flush();
  await flush();
  assert.deepEqual(names(tab.notAssignedRows), ["tester", "backend"]);
  const other = await gateway.getRepositoryAgents("r2");
  await gateway.unassignAgent({ repositoryId: "r2", agent: "reviewer", listVersion: other.listVersion });
  await flush();
  assert.deepEqual(names(tab.assignedRows), ["reviewer"]);
});

test("a resync re-reads the list", async () => {
  const { tab, gateway, memory } = await open();
  const seen = await gateway.getRepositoryAgents("r1");
  tab.busy = false;
  await gateway.unassignAgent({ repositoryId: "r1", agent: "tester", listVersion: seen.listVersion });
  tab.list = seen; // the page missed the event
  memory.resync();
  await flush();
  await flush();
  assert.deepEqual(names(tab.notAssignedRows), ["tester", "backend"]);
});

test("a load that fails says so with its code and a next step", async () => {
  const memory = memoryRepositoryAgents(makeWorld());
  const el = document.createElement("section");
  Object.assign(el.dataset, { projectId: "p1", repositoryId: "ghost" });
  const m = mount(repositoryAgentsTab({ gateway: memory.gateway }), { el });
  await m.instance.init();
  assert.equal(m.instance.error.code, "REPOSITORY_NOT_FOUND");
  assert.equal(m.instance.loaded, true);
});
