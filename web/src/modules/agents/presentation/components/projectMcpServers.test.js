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

test("the section lists every catalog server and ticks the ones the project has", async () => {
  const { page } = await open();
  assert.deepEqual(page.rows.map((r) => [r.name, r.checked]), [["github", true], ["linear", false], ["postgres", false]]);
  assert.equal(page.count, "1 of 3 selected");
  assert.equal(page.changed, false);
  assert.equal(page.cannotSave, true);
});

test("ticking a server changes the selection; Cancel puts the saved list back", async () => {
  const { page } = await open();
  page.toggle("linear");
  assert.equal(page.changed, true);
  assert.equal(page.cannotSave, false);
  assert.deepEqual(page.example.withoutAgent, ["github", "linear"]);
  page.cancel();
  assert.equal(page.changed, false);
  assert.deepEqual(page.selected, ["github"]);
});

test("Save writes the project's list, says so in a toast, and Undo puts the old list back", async () => {
  const { page, other } = await open();
  page.toggle("linear");
  page.toggle("postgres");
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
  page.toggle("postgres");
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
  page.toggle("linear");
  await other.saveMcpServer({ server: stdio("fresh"), agents: [], projects: [] });
  await timers.drain();
  assert.deepEqual(page.rows.map((r) => r.name), ["github", "linear", "postgres", "fresh"]);
  assert.deepEqual(page.selected, ["github", "linear"]);
  assert.equal(page.hasBanner, false);
});

test("a save that names a server deleted meanwhile is refused in plain words and reads again", async () => {
  const { page, other } = await open();
  page.toggle("linear");
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
  page.toggle("linear");
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
