import { mount } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { catalogGateway } from "../../infrastructure/catalog-gateway.js";
import { memoryCatalog } from "../../infrastructure/memory-gateway.js";
import { makeWorld } from "../../testing/fixtures.js";
import { projectMcpServers } from "./projectMcpServers.js";

file("agents/presentation/components/projectMcpServers");

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

const stdio = (name, over = {}) => ({ name, transport: "stdio", command: "npx", args: [], env: {}, url: "", headers: {}, ...over });

async function open({ extra = ["linear", "postgres"] } = {}) {
  const world = makeWorld();
  world.probeTools = { github: ["create_issue", "search"] };
  const { gateway, server } = memoryCatalog(world);
  const other = catalogGateway(server.api);
  for (const name of extra) await other.saveMcpServer({ server: stdio(name), agents: [], projects: [] });
  const el = document.createElement("section");
  el.dataset.projectId = "p1";
  el.dataset.projectName = "harness";
  const timers = fakeTimers();
  const m = mount(projectMcpServers({ gateway, timers }), { el });
  await m.instance.init();
  return { ...m, page: m.instance, server, other, timers };
}

test("the section lists every catalog server with its state: Added for the project's own, Agent's choice for the rest", async () => {
  const { page } = await open();
  assert.deepEqual(page.rows.map((r) => [r.name, r.state]), [["github", "added"], ["linear", "agent"], ["postgres", "agent"]]);
  assert.equal(page.count, "3 servers: 1 Added, 2 Agent’s choice, 0 Banned");
  assert.equal(page.changed, false);
  assert.equal(page.cannotSave, true);
});

test("ticking a server changes the selection; Cancel puts the saved list back", async () => {
  const { page } = await open();
  page.setState("linear", "added");
  assert.equal(page.changed, true);
  assert.equal(page.cannotSave, false);
  assert.deepEqual(page.example.withoutAgent, ["github", "linear"]);
  page.cancel();
  assert.equal(page.changed, false);
  assert.deepEqual(page.selected, ["github"]);
});

test("Save writes the project's list, says so in a toast, and Undo puts the old list back", async () => {
  const { page, other } = await open();
  page.setState("linear", "added");
  page.setState("postgres", "added");
  await page.save();
  assert.deepEqual(await other.getProjectMcpServers("p1"), ["github", "linear", "postgres"]);
  assert.equal(page.toast.text, "Saved — harness now gives 3 MCP servers to its sessions.");
  assert.ok(/Running sessions keep/.test(page.toast.note));
  assert.equal(page.changed, false);
  await page.undoSave();
  assert.deepEqual(await other.getProjectMcpServers("p1"), ["github"]);
  assert.equal(page.toast.hasUndo, false);
  assert.ok(/^Undone/.test(page.toast.text));
});

test("a server deleted while the page is open is named in a banner and leaves the list, keeping what else was ticked", async () => {
  const { page, other, timers } = await open();
  await other.saveMcpServer({ previousName: "linear", server: stdio("linear"), agents: [], projects: ["p1"] });
  await timers.drain();
  assert.deepEqual(page.saved, ["github", "linear"]);
  page.setState("postgres", "added");
  await other.deleteMcpServer("linear");
  await timers.drain();
  assert.equal(page.bannerText, "“linear” was deleted from the catalog and was taken off this project too. The list below is up to date.");
  assert.deepEqual(page.rows.map((r) => r.name), ["github", "postgres"]);
  assert.deepEqual(page.selected, ["github", "postgres"]);
  assert.deepEqual(page.saved, ["github"]);
  page.dismissBanner();
  assert.equal(page.hasBanner, false);
});

test("a server added in another tab appears without a reload and without disturbing the ticks", async () => {
  const { page, other, timers } = await open();
  page.setState("linear", "added");
  await other.saveMcpServer({ server: stdio("fresh"), agents: [], projects: [] });
  await timers.drain();
  assert.deepEqual(page.rows.map((r) => r.name), ["github", "linear", "postgres", "fresh"]);
  assert.deepEqual(page.selected, ["github", "linear"]);
  assert.equal(page.hasBanner, false);
});

test("a save that names a server deleted meanwhile is refused in plain words and reads again", async () => {
  const { page, other } = await open();
  page.setState("linear", "added");
  await other.deleteMcpServer("linear");
  await page.save();
  assert.ok(/linear/.test(page.refusal.message));
  assert.equal(page.toast, null);
});

test("a broken catalog turns Save off and says why", async () => {
  const { page, server, timers } = await open();
  server.breakFile("mcp-servers.json", 3, 7, "unexpected comma");
  page.windowFocused();
  await timers.drain();
  page.setState("linear", "added");
  assert.equal(page.isBroken, true);
  assert.equal(page.cannotSave, true);
  assert.ok(/^mcp-servers.json does not parse/.test(page.brokenNote));
});

test("an empty catalog is the empty state, with nothing to save", async () => {
  const { page, other, timers } = await open({ extra: [] });
  assert.equal(page.isEmpty, false);
  await other.deleteMcpServer("github");
  page.windowFocused();
  await timers.drain();
  assert.equal(page.isEmpty, true);
  assert.equal(page.hasRows, false);
  assert.equal(page.count, "");
});

const ev = (dataset) => ({ currentTarget: { dataset } });
/** The world with github probed (two tools, one disabled by default) and linear never probed. */
async function openWithTools() {
  const o = await open();
  await o.other.saveMcpServer({ previousName: "github", server: { ...stdio("github"), disabledTools: ["create_issue"] }, agents: ["ux-designer"], projects: ["p1"] });
  await o.timers.drain();
  return o;
}

test("a server with known tools lists each with its default and what is in effect; a never-probed one says its tools are unknown", async () => {
  const { page, other, timers } = await openWithTools();
  await other.saveMcpServer({ server: stdio("down", { command: "missing-cmd" }), agents: [], projects: [] });
  await timers.drain();
  const github = page.rows[0];
  const linear = page.rows.find((r) => r.name === "down");
  assert.deepEqual(github.tools.rows.map((t) => [t.name, t.choice, t.effective]), [["create_issue", "default", "disabled"], ["search", "default", "enabled"]]);
  assert.equal(linear.toolsUnknown, true);
  assert.equal(linear.hasToolRows, false);
});

test("picking a state and a tool override marks the page changed; the write sends every server's state and overrides; Reset to defaults clears a server's", async () => {
  const { page, other } = await openWithTools();
  page.pickState(ev({ name: "linear", state: "banned" }));
  page.pickTool(ev({ name: "github", tool: "create_issue", choice: "enabled" }));
  assert.equal(page.mcpChanged, true);
  assert.equal(page.rows[0].canReset, true);
  assert.equal(page.rows[1].banWarning.startsWith("Banned:"), true);
  page.resetTools(ev({ name: "github" }));
  page.pickTool(ev({ name: "github", tool: "search", choice: "disabled" }));
  await page.save();
  const settings = await other.getProjectMcpSettings("p1");
  assert.deepEqual(settings.servers.map((s) => s.state), ["added", "banned", "agent"]);
  assert.deepEqual(settings.servers[0].tools.map((t) => [t.name, t.override, t.effective]), [["create_issue", "default", "disabled"], ["search", "disabled", "disabled"]]);
  assert.equal(page.toast.text, "Saved — harness now gives 1 MCP server to its sessions and bans 1.");
  assert.equal(page.mcpChanged, false);
  await page.undoSave();
  assert.deepEqual((await other.getProjectMcpSettings("p1")).servers.map((s) => s.state), ["added", "agent", "agent"], "Undo puts the bans and overrides back");
});

test("banning is not refused and the example leaves the banned server out", async () => {
  const { page } = await openWithTools();
  page.setState("github", "banned");
  assert.deepEqual(page.example.withoutAgent, []);
  assert.equal(page.example.bannedText, "Banned in this project, so no session gets it: github");
  assert.equal(page.cannotSave, false);
  await page.save();
  assert.equal(page.refusal, null);
});

test("Refresh tools adopts the server's new tool list and keeps the states and overrides not yet saved", async () => {
  const { page, server } = await openWithTools();
  page.setState("linear", "banned");
  page.pickTool(ev({ name: "github", tool: "search", choice: "disabled" }));
  server.api.post("/save_catalog_mcp_server", { previous_name: "linear", server: { ...stdio("linear") }, agents: [], projects: [] });
  await page.refreshTools(ev({ name: "linear" }));
  assert.equal(page.rows[1].hasToolRows, false, "a banned server shows no tools, but its settings were read");
  assert.equal(page.refreshNotes.linear.text, "Probed the server: 1 tool found.");
  assert.equal(page.refreshingName, "");
  page.setState("linear", "agent");
  assert.deepEqual(page.rows[1].tools.rows.map((t) => t.name), ["search"]);
  page.setState("linear", "banned");
  assert.equal(page.draft.github.overrides.search, "disabled");
  assert.equal(page.draft.linear.state, "banned");
});

test("a failed refresh says why in the row and changes nothing", async () => {
  const { page } = await openWithTools();
  page.setState("linear", "banned");
  await page.refreshTools(ev({ name: "ghost" }));
  assert.ok(/ghost/.test(page.refreshNotes.ghost.text));
  assert.equal(page.draft.linear.state, "banned");
});

test("a server deleted while the page is open leaves the settings and is named, whatever its state was", async () => {
  const { page, other, timers } = await open();
  await other.setProjectMcpSettings("p1", [{ name: "github", state: "added" }, { name: "linear", state: "banned" }]);
  await timers.drain();
  await other.deleteMcpServer("linear");
  await timers.drain();
  assert.equal(page.bannerText, "“linear” was deleted from the catalog and was taken off this project too. The list below is up to date.");
});
