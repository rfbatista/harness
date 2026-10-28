import { StructuredError } from "../../../../shared/domain/errors.js";
import { mount } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { catalogGateway } from "../../infrastructure/catalog-gateway.js";
import { memoryCatalog } from "../../infrastructure/memory-gateway.js";
import { makeWorld } from "../../testing/fixtures.js";
import { projectMcpServers } from "./projectMcpServers.js";

file("agents/presentation/components/projectAgentsSection");

function fakeTimers() {
  const pending = new Map();
  let id = 0;
  return {
    setTimeout: (fn) => { pending.set(++id, fn); return id; },
    clearTimeout: (n) => { pending.delete(n); },
    async drain() {
      for (let i = 0; i < 5 && pending.size > 0; i++) {
        const fns = [...pending.values()];
        pending.clear();
        for (const fn of fns) await fn();
      }
    },
  };
}

const stdio = (name) => ({ name, transport: "stdio", command: "npx", args: [], env: {}, url: "", headers: {} });
const agent = (name, description = "Does things.") => ({ name, description, aliases: [], entries: [], mcpServers: [], allowedTools: [] });
const refusal = (code, message, details) => () => { throw new StructuredError(code, message, 400, details); };

/** @param {{ more?: number, wrap?: (gateway: any) => any, mode?: object }} [options] */
async function open({ more = 0, wrap = (g) => g, saved } = {}) {
  const world = makeWorld();
  if (saved) world.projectAgents = { p1: saved };
  const { gateway, server } = memoryCatalog(world);
  const other = catalogGateway(server.api);
  await other.saveMcpServer({ server: stdio("linear"), agents: [], projects: [] });
  for (let i = 0; i < more; i++) await other.saveAgent({ agent: agent(`extra-${String(i).padStart(2, "0")}`, `Extra ${i}`) });
  const el = document.createElement("div");
  el.dataset.projectId = "p1";
  el.dataset.projectName = "harness";
  const timers = fakeTimers();
  const m = mount(projectMcpServers({ gateway: wrap(gateway), timers }), { el });
  await m.instance.init();
  const calls = () => server.requests.filter(([method, path]) => method === "POST" && /set_project/.test(path)).map(([, path]) => path.slice(1));
  return { ...m, page: m.instance, server, other, timers, calls };
}

test("a project starts with All agents: the list is hidden, the count says all, nothing is unsaved", async () => {
  const { page } = await open();
  assert.equal(page.agentMode, "all");
  assert.equal(page.showAgentChecklist, false);
  assert.equal(page.agentsCount, "All 2 enabled");
  assert.ok(/including agents created later/.test(page.agentsHint));
  assert.equal(page.changed, false);
  assert.equal(page.cannotSave, true);
});

test("All to Selected starts with every agent ticked; unticking one is unsaved and saves with Undo", async () => {
  const { page, other, calls } = await open();
  page.chooseSelected();
  assert.deepEqual(page.agentRows.map((r) => [r.name, r.description, r.checked]), [["ux-designer", "Designs screens.", true], ["backend", "No description", true]]);
  assert.equal(page.agentsCount, "2 of 2 enabled");
  assert.equal(page.agentsDirty, true, "the mode itself is a change");
  page.toggleAgent("backend");
  assert.equal(page.agentsCount, "1 of 2 enabled");
  await page.save();
  assert.deepEqual(calls(), ["set_project_agents"], "the MCP list did not change, so it is not written");
  assert.deepEqual(await other.getProjectAgents("p1"), { mode: "selected", agents: ["ux-designer"] });
  assert.equal(page.toast.text, "Saved — harness now enables 1 agent.");
  assert.equal(page.changed, false);
  await page.undoSave();
  assert.deepEqual(await other.getProjectAgents("p1"), { mode: "all", agents: [] });
  assert.ok(/^Undone — harness enables all agents again\./.test(page.toast.text));
  assert.equal(page.agentMode, "all");
});

test("switching back to All keeps the list but ignores it: nothing is unsaved and it is not written", async () => {
  const { page, calls } = await open();
  page.chooseSelected();
  page.toggleAgent("ux-designer");
  page.chooseAll();
  assert.equal(page.agentsDirty, false);
  assert.equal(page.showAgentChecklist, false);
  page.chooseSelected();
  assert.deepEqual(page.agentTicked, ["backend"], "the kept list is back, not every agent");
  page.chooseAll();
  page.toggle("linear");
  await page.save();
  assert.deepEqual(calls(), ["set_project_mcp_servers"]);
});

test("an empty Selected list warns, Tick all fills it, and saving it empty is allowed", async () => {
  const { page, other } = await open({ saved: { mode: "selected", agents: ["backend"] } });
  assert.equal(page.showEmptyAgents, false);
  page.toggleAgent("backend");
  assert.equal(page.showEmptyAgents, true);
  assert.equal(page.tickAllLabel, "Tick all 2");
  await page.save();
  assert.deepEqual(await other.getProjectAgents("p1"), { mode: "selected", agents: [] });
  assert.equal(page.toast.text, "Saved — harness now enables 0 agents.");
  assert.equal(page.showEmptyAgents, true, "still empty after the save, still warned");
  page.tickAll();
  assert.equal(page.showEmptyAgents, false);
  assert.deepEqual(page.agentTicked, ["ux-designer", "backend"]);
});

test("Cancel puts the saved mode and list back, for both lists", async () => {
  const { page } = await open();
  page.chooseSelected();
  page.toggle("linear");
  page.cancel();
  assert.equal(page.agentMode, "all");
  assert.deepEqual(page.selected, ["github"]);
  assert.equal(page.changed, false);
});

test("one dirty list is enough to turn Save on, and one Save writes both lists, agents first", async () => {
  const { page, other, calls } = await open();
  page.toggle("linear");
  assert.equal(page.cannotSave, false);
  page.chooseSelected();
  page.toggleAgent("backend");
  await page.save();
  assert.deepEqual(calls(), ["set_project_agents", "set_project_mcp_servers"]);
  assert.deepEqual(await other.getProjectMcpServers("p1"), ["github", "linear"]);
  assert.equal(page.toast.text, "Saved — harness now enables 1 agent and gives 2 MCP servers to its sessions.");
  await page.undoSave();
  assert.deepEqual(await other.getProjectAgents("p1"), { mode: "all", agents: [] });
  assert.deepEqual(await other.getProjectMcpServers("p1"), ["github"]);
});

test("naming an agent the catalog lacks is refused with the names, and the list stays unsaved", async () => {
  const { page } = await open({ saved: { mode: "selected", agents: ["backend"] }, wrap: (g) => ({ ...g, setProjectAgents: refusal("UNKNOWN_REFERENCE", "unknown: backend", { names: ["backend"] }) }) });
  page.toggleAgent("ux-designer");
  await page.save();
  assert.equal(page.agentsRefusal.message, "These do not exist: backend.");
  assert.equal(page.agentsDescribedBy, "settings-agents-hint settings-agents-refusal");
  assert.equal(page.agentsDirty, true);
  assert.equal(page.cannotSave, false);
});

test("agents saved and MCP refused: it says so, shows the refusal and only the MCP list stays unsaved", async () => {
  const { page, other } = await open({ wrap: (g) => ({ ...g, setProjectMcpServers: refusal("UNKNOWN_REFERENCE", "unknown: linear", { names: ["linear"] }) }) });
  page.chooseSelected();
  page.toggleAgent("backend");
  page.toggle("linear");
  await page.save();
  assert.deepEqual(await other.getProjectAgents("p1"), { mode: "selected", agents: ["ux-designer"] });
  assert.equal(page.partial.text, "Enabled agents were saved. MCP servers were not: These do not exist: linear.");
  assert.equal(page.refusal.message, "These do not exist: linear.");
  assert.equal(page.agentsRefusal, null);
  assert.equal(page.agentsDirty, false);
  assert.equal(page.mcpChanged, true);
  assert.equal(page.cannotSave, false, "Save stays on for the unsaved part");
  assert.equal(page.toast.text, "Saved — harness now enables 1 agent.");
});

test("MCP saved and agents refused: it says so, shows the refusal and only the agents stay unsaved", async () => {
  const { page, other, calls } = await open({ wrap: (g) => ({ ...g, setProjectAgents: refusal("INVALID_INPUT", "mode is invalid", {}) }) });
  page.chooseSelected();
  page.toggle("linear");
  await page.save();
  assert.deepEqual(calls(), ["set_project_mcp_servers"], "the refused agents call never reached the server");
  assert.deepEqual(await other.getProjectMcpServers("p1"), ["github", "linear"]);
  assert.equal(page.partial.text, "MCP servers were saved. Enabled agents were not: mode is invalid");
  assert.ok(page.agentsRefusal);
  assert.equal(page.refusal, null);
  assert.equal(page.mcpChanged, false);
  assert.equal(page.agentsDirty, true);
  assert.equal(page.cannotSave, false);
  assert.equal(page.toast.text, "Saved — harness now gives 2 MCP servers to its sessions.");
});

test("when both calls are refused nothing saved, so no part is named as saved", async () => {
  const no = refusal("INVALID_INPUT", "no", {});
  const { page } = await open({ wrap: (g) => ({ ...g, setProjectAgents: no, setProjectMcpServers: no }) });
  page.chooseSelected();
  page.toggle("linear");
  await page.save();
  assert.equal(page.partial, null);
  assert.ok(page.agentsRefusal && page.refusal);
  assert.equal(page.toast, null);
});

test("another tab's change to the project's agents shows up; a deleted agent is named and leaves the list", async () => {
  const { page, other, timers } = await open();
  await other.setProjectAgents("p1", { mode: "selected", agents: ["backend", "ux-designer"] });
  await timers.drain();
  assert.equal(page.agentMode, "selected");
  assert.deepEqual(page.savedAgents.agents, ["ux-designer", "backend"]);
  await other.deleteAgent("backend");
  await timers.drain();
  assert.equal(page.agentBannerText, "“backend” was deleted from the catalog and was taken off this project too. The list below is up to date.");
  assert.deepEqual(page.agentRows.map((r) => r.name), ["ux-designer"]);
  assert.deepEqual(page.agentTicked, ["ux-designer"]);
  page.dismissAgentBanner();
  assert.equal(page.hasAgentBanner, false);
});

test("an unsaved change survives the catalog changing, and a new agent is not ticked in Selected", async () => {
  const { page, other, timers } = await open({ saved: { mode: "selected", agents: ["backend"] } });
  page.toggleAgent("ux-designer");
  await other.saveAgent({ agent: agent("fresh") });
  await timers.drain();
  assert.deepEqual(page.agentRows.map((r) => [r.name, r.checked]), [["ux-designer", true], ["backend", true], ["fresh", false]]);
  assert.equal(page.agentsDirty, true);
});

test("a broken catalog turns Save off for the agents too", async () => {
  const { page, server, timers } = await open();
  server.breakFile("agents.json", 1, 2, "unexpected comma");
  page.windowFocused();
  await timers.drain();
  page.chooseSelected();
  assert.equal(page.cannotSave, true);
});

test("past fifteen agents the checklist is a search combobox with chips", async () => {
  const { page } = await open({ more: 14, saved: { mode: "selected", agents: ["backend"] } });
  assert.equal(page.agentRows.length, 16);
  assert.equal(page.showAgentCombobox, true);
  assert.equal(page.showAgentChecklist, false);
  assert.deepEqual(page.agentPick.chips.map((c) => c.name), ["backend"]);
  page.agentQuery = "extra-0";
  page.agentInput();
  assert.deepEqual(page.agentPick.options.map((o) => o.key).slice(0, 3), ["extra-00", "extra-01", "extra-02"]);
  assert.equal(page.agentPick.activeId, "agent-opt-0");
  page.agentKey({ key: "ArrowDown", preventDefault() {} });
  page.agentKey({ key: "Enter", preventDefault() {} });
  assert.deepEqual(page.agentTicked, ["backend", "extra-01"]);
  assert.equal(page.agentsCount, "2 of 16 enabled");
  assert.equal(page.agentQuery, "");
  page.agentKey({ key: "Backspace", preventDefault() {} });
  assert.deepEqual(page.agentTicked, ["backend"], "Backspace in the empty input removes the last chip");
});
