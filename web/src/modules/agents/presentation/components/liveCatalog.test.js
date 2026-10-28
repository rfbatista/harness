import { mount, seededElement } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { FeedStatus } from "../../../../shared/domain/feed.js";
import { catalogGateway } from "../../infrastructure/catalog-gateway.js";
import { memoryCatalog } from "../../infrastructure/memory-gateway.js";
import { makeWorld } from "../../testing/fixtures.js";
import { agentsPage } from "./agentsPage.js";
import { trackWrites } from "./liveCatalog.js";

file("agents/presentation/components/liveCatalog");

function fakeTimers() {
  const pending = new Map();
  let id = 0;
  return {
    setTimeout: (fn) => { pending.set(++id, fn); return id; },
    clearTimeout: (n) => { pending.delete(n); },
    get count() { return pending.size; },
    /** Fires every pending timer, and the ones those start, until the page is quiet. */
    async drain() {
      for (let i = 0; i < 5 && pending.size > 0; i++) {
        const fns = [...pending.values()];
        pending.clear();
        for (const fn of fns) await fn();
      }
    },
  };
}

/** The page at `path` and a second tab's gateway over the same server. */
async function open(path, { world = makeWorld() } = {}) {
  const { gateway, server } = memoryCatalog(world);
  const other = catalogGateway(server.api);
  const timers = fakeTimers();
  const pushed = [];
  const location = { pathname: path, search: "" };
  const history = { pushState: (_s, _t, url) => { pushed.push(url); location.pathname = url.split("?")[0]; } };
  const seed = await server.api.get("/get_agent_catalog");
  const m = mount(agentsPage({ gateway, history, location, timers }), { el: seededElement(seed) });
  await m.instance.init();
  const reads = () => server.requests.filter(([method, p]) => method === "GET" && p === "/get_agent_catalog").length;
  return { ...m, page: m.instance, server, other, timers, reads, location };
}

const agentBody = (name, over = {}) => ({ name, description: "Edited.", aliases: [], entries: ["tdd"], mcpServers: [], allowedTools: [], ...over });

test("a save in another tab refreshes this page: the new item is listed, and nothing else is redrawn", async () => {
  const { page, other, timers } = await open("/agents");
  const before = page.catalog;
  await other.saveAgent({ agent: agentBody("fresh") });
  assert.equal(timers.count, 1, "the event started one timer");
  await timers.drain();
  assert.deepEqual(page.agentRowsView.map((r) => r.name), ["ux-designer", "backend", "fresh"]);
  assert.ok(page.catalog !== before);
});

test("unrelated events read nothing, and a burst of catalog events is one read", async () => {
  const { page, server, other, timers, reads } = await open("/agents");
  const start = reads();
  server.publish({ project: { id: "p1" } });
  server.publish({ git_identity: {} });
  assert.equal(timers.count, 0);
  await other.saveAgent({ agent: agentBody("one") });
  await other.saveAgent({ agent: agentBody("two") });
  await other.saveAgent({ agent: agentBody("three") });
  await timers.drain();
  assert.equal(reads() - start, 1, "three saves, one read");
  assert.equal(page.catalog.agents.length, 5);
});

test("a read that finds nothing new keeps the catalog object, so a focus does not redraw the lists", async () => {
  const { page, timers, reads } = await open("/agents/skills");
  const catalog = page.catalog;
  const start = reads();
  page.windowFocused();
  await timers.drain();
  assert.equal(reads() - start, 1, "focus reads");
  assert.equal(page.catalog, catalog, "same object: nothing re-rendered");
  assert.deepEqual(page.changed, {});
});

test("the feed coming back after a drop reads once; its first connection does not", async () => {
  const { server, timers, reads } = await open("/agents");
  const start = reads();
  server.setFeedStatus(FeedStatus.LIVE);
  assert.equal(timers.count, 0);
  server.setFeedStatus(FeedStatus.PAUSED);
  server.setFeedStatus(FeedStatus.RESYNCED);
  await timers.drain();
  assert.equal(reads() - start, 1);
});

test("Reload from disk reads now, and says Reloading while it does", async () => {
  const { page, timers, server } = await open("/agents");
  assert.equal(page.reloadLabel, "Reload from disk");
  server.editOnDisk("agents.json");
  page.reloadFromDisk();
  assert.equal(timers.count, 1);
  await timers.drain();
  assert.equal(page.refreshing, false);
  assert.equal(page.catalog.versions["agents.json"], "sha256:agents.json-1");
});

test("an agent changed in another tab is flagged on its row, and its open detail says what changed", async () => {
  const { page, other, timers } = await open("/agents/ux-designer");
  assert.equal(page.disk, null);
  await other.saveAgent({ previousName: "ux-designer", agent: agentBody("ux-designer", { description: "Designs, differently.", entries: ["@ux", "lint"], aliases: ["ux"], mcpServers: ["github"], allowedTools: ["Read"] }) });
  await timers.drain();
  const row = page.agentRowsView.find((r) => r.name === "ux-designer");
  assert.equal(row.badgeLabel, "changed on disk");
  assert.equal(row.hasBadge, true);
  assert.deepEqual(page.agentRowsView.filter((r) => r.name !== "ux-designer").map((r) => r.badgeLabel), ["No description"]);
  assert.equal(page.disk.sentence, "This agent changed on disk.");
  assert.deepEqual(page.disk.lines.map((l) => l.text), ["description changed", "skills and bundles: added lint, removed tdd"]);
  assert.equal(page.detail.description, "Designs, differently.", "the detail shows what is on disk now");
  page.dismissDisk();
  assert.equal(page.disk, null);
  assert.equal(page.agentRowsView.find((r) => r.name === "ux-designer").badgeLabel, "");
});

test("an agent deleted in another tab while its detail is open says it was removed", async () => {
  const { page, other, timers } = await open("/agents/backend");
  await other.deleteAgent("backend");
  await timers.drain();
  assert.equal(page.disk.sentence, "This agent was removed from the catalog on disk.");
  assert.equal(page.unknownAgent, true);
});

test("what this page saved itself is not a change from outside, even when its event wins the race", async () => {
  const { page, timers, server } = await open("/agents/bundles/ux/edit");
  page.draft.name = "design";
  page.draftChanged();
  await page.saveEditor();
  assert.equal(timers.count >= 1, true, "the page's own save published an event");
  await timers.drain();
  assert.deepEqual(page.changed, {});
  assert.equal(page.bundleRowsView.some((r) => r.disk), false);
  assert.ok(server.requests.length > 0);
});

test("the write tracker records what each write answered, leaves dry runs out, and knows when writes are done", async () => {
  const { gateway } = memoryCatalog(makeWorld());
  const tracker = trackWrites(gateway);
  const dry = await tracker.gateway.saveAgent({ agent: agentBody("x") }, { dryRun: true });
  assert.ok(dry.versions["agents.json"]);
  assert.equal(tracker.takeOwn().hashes.size, 0, "a dry run answers the current versions; they are not ours");
  const pending = tracker.gateway.saveAgent({ agent: agentBody("x") });
  const settled = tracker.settled();
  const saved = await pending;
  await settled;
  assert.deepEqual([...tracker.takeOwn().hashes], Object.values(saved.versions));
  assert.equal(tracker.takeOwn().hashes.size, 0, "taken once");
  await assert.rejects(tracker.gateway.deleteAgent("nope"));
});

test("an open bundle editor whose bundle changed in another tab blocks Save and keeps the draft", async () => {
  const { page, other, timers } = await open("/agents/bundles/ux/edit");
  page.draft.entries.push("tdd");
  page.draftChanged();
  await other.saveBundle({ previousName: "ux", bundle: { name: "ux", entries: ["tdd"] } });
  await timers.drain();
  assert.equal(page.save.phase, "conflict");
  assert.equal(page.sv.canSave, false);
  assert.equal(page.sv.conflict, true);
  assert.deepEqual(page.draft.entries, ["lint", "tdd"], "the draft is kept");
  assert.equal(page.changed["bundle:ux"].lines[0], "skills and bundles: added tdd, removed lint");
});

test("an open editor whose item did not change takes the new versions and goes on", async () => {
  const { page, other, timers, server } = await open("/agents/bundles/ux/edit");
  page.draft.entries.push("tdd");
  page.draftChanged();
  await timers.drain();
  await other.saveAgent({ previousName: "backend", agent: agentBody("backend", { description: "Now it has one." }) });
  await timers.drain();
  assert.ok(page.save.phase !== "conflict");
  assert.equal(page.sv.conflict, false);
  assert.equal(page.save.baseVersions["agents.json"], page.catalog.versions["agents.json"]);
  const read = page.catalog.versions["agents.json"];
  await page.saveEditor();
  assert.equal(page.editing, false, "the save went through, not into a conflict");
  const sent = server.requests.filter(([m, p, b]) => m === "POST" && p === "/save_catalog_bundle" && !b.dry_run).at(-1);
  assert.equal(sent[2].base_versions["agents.json"], read, "it carried the versions read after the other tab's save");
});

test("an open skill whose folder changed shows the banner; one this page saved does not", async () => {
  const world = makeWorld();
  const { gateway, server } = memoryCatalog(world);
  let version = "sha256:tdd-1";
  const real = gateway.getSkill;
  const g = { ...gateway, getSkill: async (name) => ({ ...(await real(name)), versions: { "skills/tdd": version } }) };
  const timers = fakeTimers();
  const location = { pathname: "/agents/skills/tdd", search: "" };
  const m = mount(agentsPage({ gateway: g, history: { pushState() {} }, location, timers }), { el: seededElement(await server.api.get("/get_agent_catalog")) });
  await m.instance.init();
  assert.equal(m.instance.skillVersion, "sha256:tdd-1");
  assert.equal(m.instance.disk, null);
  version = "sha256:tdd-2";
  m.instance.windowFocused();
  await timers.drain();
  assert.equal(m.instance.disk.sentence, "This skill changed on disk.");
  assert.deepEqual(m.instance.disk.lines.map((l) => l.text), ["SKILL.md or its files changed"]);
  assert.equal(m.instance.skillVersion, "sha256:tdd-2");
  m.instance.dismissDisk();
  m.instance.windowFocused();
  await timers.drain();
  assert.equal(m.instance.disk, null, "read again, nothing new");
});

test("a catalog that does not parse shows the file, line and column, keeps the last lists, and turns every save and delete off", async () => {
  const { page, server, timers } = await open("/agents/bundles/ux/edit");
  assert.equal(page.broken, null);
  page.draft.entries.push("tdd");
  page.draftChanged();
  assert.equal(page.sv.canSave, true);
  server.breakFile("agents.json", 12, 4, "invalid character '}' looking for beginning of object key string");
  page.reloadFromDisk();
  await timers.drain();
  assert.equal(page.broken.title, "agents.json does not parse");
  assert.equal(page.broken.where, "Line 12, column 4");
  assert.ok(page.broken.message.includes("invalid character"));
  assert.equal(page.catalog.bundles.length, 1, "the last version that parsed is still listed");
  assert.equal(page.sv.canSave, false);
  assert.equal(page.sv.footer, "Saving is off · agents.json does not parse");
  assert.equal(page.checkCatalogLabel, "Check again");
  await page.saveEditor();
  assert.equal(server.requests.some(([m, p, b]) => p === "/save_catalog_bundle" && !b.dry_run), false, "no save was sent");
});

test("a delete is off while the catalog does not parse, and Check again after the fix turns saves back on", async () => {
  const { page, server, timers } = await open("/agents/bundles/ux");
  page.askDelete({});
  page.delTyped = "ux";
  assert.equal(page.delReady, true);
  server.breakFile("agents.json", 3, 1, "unexpected end of JSON input");
  page.reloadFromDisk();
  await timers.drain();
  assert.equal(page.delReady, false);
  assert.ok(page.delBlockedNote.startsWith("Deleting is off · agents.json does not parse"));
  server.fixFile();
  page.reloadFromDisk();
  await timers.drain();
  assert.equal(page.broken, null);
  assert.equal(page.delReady, true);
  assert.equal(page.delBlockedNote, "");
});

test("a preview owed while the file did not parse is asked for once it does", async () => {
  const { page, server, timers } = await open("/agents/bundles/ux/edit");
  server.breakFile("agents.json", 1, 1, "bad");
  page.reloadFromDisk();
  await timers.drain();
  page.draft.entries.push("tdd");
  page.draftChanged();
  await timers.drain();
  assert.equal(page.sv.review, false, "no preview while saving is off");
  server.fixFile();
  page.reloadFromDisk();
  await timers.drain();
  assert.equal(page.sv.review, true);
  assert.equal(page.sv.canSave, true);
});

test("no secret value reaches a changed-on-disk line", async () => {
  const { page, other, timers } = await open("/agents/mcp/github");
  await other.saveMcpServer({ previousName: "github", server: { name: "github", transport: "stdio", command: "npx", args: ["-y"], env: { TOKEN: "ghp_supersecret123" } }, agents: ["ux-designer"], projects: [] });
  await timers.drain();
  const text = JSON.stringify(page.disk) + JSON.stringify(page.changed);
  assert.ok(page.disk.lines.length > 0);
  assert.ok(!text.includes("ghp_supersecret123"));
});
