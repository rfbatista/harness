import { StructuredError } from "../../../../shared/domain/errors.js";
import { mount, seededElement } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { memoryCatalog } from "../../infrastructure/memory-gateway.js";
import { makeWorld } from "../../testing/fixtures.js";
import { agentsPage } from "./agentsPage.js";

file("agents/presentation/components/agentsPage");

function fakeHistory(path) {
  const pushed = [];
  return { pushed, location: { pathname: path }, history: { pushState: (_s, _t, url) => { pushed.push(url); } } };
}

function page({ path = "/agents", seed, gateway = memoryCatalog(makeWorld()).gateway } = {}) {
  const el = seed ? seededElement(seed) : document.createElement("main");
  const route = fakeHistory(path);
  const m = mount(agentsPage({ gateway, history: route.history, location: route.location }), { el });
  return { ...m, route };
}

test("the first paint's seed is the catalog: the root and the tab counts, without a request", async () => {
  const { gateway, server } = memoryCatalog(makeWorld());
  const seed = await server.api.get("/get_agent_catalog");
  const { instance } = page({ seed, gateway });
  await instance.init();
  assert.equal(instance.root, "/data/agents");
  assert.deepEqual([instance.tabState.agents.count, instance.tabState.skills.count, instance.tabState.mcp.count, instance.tabState.tools.count], ["2", "2", "1", "2"]);
  assert.equal(server.requests.length, 1, "only the seed read above");
});

test("without a seed the page reads the catalog itself", async () => {
  const { gateway } = memoryCatalog(makeWorld());
  const { instance } = page({ gateway });
  await instance.init();
  assert.equal(instance.root, "/data/agents");
  assert.equal(instance.error, null);
});

test("when the read fails the page still works, says why, and offers to check again", async () => {
  const gateway = { decodeCatalog: () => { throw new Error("no seed"); }, getCatalog: async () => { throw new StructuredError("NETWORK", "The harness server is not reachable."); } };
  const { instance } = page({ gateway });
  await instance.init();
  assert.equal(instance.error.message, "The harness server is not reachable.");
  assert.equal(instance.root, "<data dir>/agents/");
  assert.equal(instance.tabState.agents.count, "");
  instance.select({ preventDefault() {}, currentTarget: { dataset: { tab: "skills" } } });
  assert.equal(instance.tab, "skills");
});

test("the route picks the tab, and the New button follows it", async () => {
  const { instance } = page({ path: "/agents/skills/tdd" });
  await instance.init();
  assert.equal(instance.tab, "skills");
  assert.equal(instance.item, "tdd");
  assert.equal(instance.newLabel, "New skill");
  assert.equal(instance.newHref, "/agents/skills/new");
  assert.equal(instance.newHidden, false);
});

test("choosing a tab shows it, moves the address to its route, and changes the New button; Tools has none", async () => {
  const { instance, route } = page();
  await instance.init();
  const ev = { prevented: false, preventDefault() { this.prevented = true; }, currentTarget: { dataset: { tab: "mcp" } } };
  instance.select(ev);
  assert.ok(ev.prevented, "the link does not navigate itself");
  assert.equal(instance.tab, "mcp");
  assert.deepEqual(route.pushed, ["/agents/mcp"]);
  assert.equal(instance.newLabel, "New MCP server");
  assert.deepEqual([instance.tabState.mcp.selected, instance.tabState.mcp.tabindex, instance.tabState.agents.selected, instance.tabState.agents.tabindex], [true, "0", false, "-1"]);
  assert.deepEqual([instance.tabState.mcp.hidden, instance.tabState.agents.hidden], [false, true]);
  instance.select({ preventDefault() {}, currentTarget: { dataset: { tab: "tools" } } });
  assert.equal(instance.newHidden, true);
  assert.equal(instance.newHref, "");
});

test("a modified click is left to the browser", async () => {
  const { instance, route } = page();
  await instance.init();
  const ev = { metaKey: true, preventDefault() { throw new Error("must not prevent"); }, currentTarget: { dataset: { tab: "skills" } } };
  instance.select(ev);
  assert.equal(instance.tab, "agents");
  assert.deepEqual(route.pushed, []);
});

test("arrow keys, Home and End move to the next tab and focus it", async () => {
  const { instance } = page();
  await instance.init();
  const focused = [];
  const tabKey = (k, from) => {
    const ev = { key: k, prevented: false, preventDefault() { this.prevented = true; }, currentTarget: { dataset: { tab: from }, parentElement: { querySelector: (sel) => ({ focus: () => focused.push(sel) }) } } };
    instance.tabKey(ev);
    return ev;
  };
  assert.ok(tabKey("ArrowRight", "agents").prevented);
  assert.equal(instance.tab, "skills");
  tabKey("End", "skills");
  assert.equal(instance.tab, "tools");
  tabKey("ArrowRight", "tools");
  assert.equal(instance.tab, "agents");
  tabKey("ArrowLeft", "agents");
  assert.equal(instance.tab, "tools");
  tabKey("Home", "tools");
  assert.equal(instance.tab, "agents");
  assert.equal(focused.length, 5);
  assert.deepEqual(focused.slice(0, 2), ['[data-tab="skills"]', '[data-tab="tools"]']);
  const other = tabKey("Tab", "agents");
  assert.ok(!other.prevented, "Tab keeps its meaning");
  assert.equal(instance.tab, "agents");
});

test("going back follows the address to the earlier tab", async () => {
  const { instance, route } = page({ path: "/agents/bundles" });
  await instance.init();
  assert.equal(instance.tab, "bundles");
  route.location.pathname = "/agents/tools";
  instance.popped();
  assert.equal(instance.tab, "tools");
  route.location.pathname = "/agents/skills/tdd/edit";
  instance.popped();
  assert.deepEqual([instance.tab, instance.item, instance.mode], ["skills", "tdd", "edit"]);
});

test("choosing an agent shows its detail and moves the address; a bundle group opens and closes; Copy path copies the file's path", async () => {
  const { gateway, server } = memoryCatalog(makeWorld());
  const seed = await server.api.get("/get_agent_catalog");
  const copied = [];
  const el = seededElement(seed);
  const route = fakeHistory("/agents");
  const { instance } = mount(agentsPage({ gateway, history: route.history, location: route.location, clipboard: { writeText: async (t) => { copied.push(t); } } }), { el });
  await instance.init();
  assert.deepEqual(instance.agentRowsView.map((r) => r.name), ["ux-designer", "backend"]);
  assert.equal(instance.detail, null);

  const link = { dataset: { key: "ux-designer" }, getAttribute: () => "/agents/ux-designer" };
  instance.openAgent({ preventDefault() {}, button: 0, currentTarget: link });
  assert.deepEqual(route.pushed, ["/agents/ux-designer"]);
  assert.equal(instance.detail.key, "ux-designer");
  assert.equal(instance.agentRowsView[0].current, true);

  assert.equal(instance.groups[0].expanded, true, "a lone bundle starts open");
  instance.toggleGroup({ currentTarget: { dataset: { group: "@ux" } } });
  assert.equal(instance.groups[0].expanded, false);

  await instance.menuSelect({ detail: { action: "copy-path" } });
  assert.deepEqual(copied, ["/data/agents/agents.json"]);
  assert.equal(instance.notice, "Path copied");

  instance.filter = "problems";
  assert.deepEqual(instance.agentRowsView.map((r) => r.name), ["backend"]);
});

test("an agent's detail carries its usage line, its servers' targets and the Start menu from the catalog read; an agent without usage has no line", async () => {
  const { instance } = page({ path: "/agents/ux-designer" });
  await instance.init();
  assert.equal(instance.detail.usage, "Used by 3 sessions · 1 zone");
  assert.deepEqual(instance.detail.servers.map((s) => [s.name, s.transport, s.target]), [["github", "stdio", "npx -y gh-mcp"]]);
  assert.deepEqual(instance.detail.start.projects.map((p) => [p.name, p.href]), [["harness", "/projects/p1/sessions?agent=ux-designer"]]);
  const backend = page({ path: "/agents/backend" });
  await backend.instance.init();
  assert.equal(backend.instance.detail.usage, "", "the memory gateway sends none for backend, as an older server");
});
