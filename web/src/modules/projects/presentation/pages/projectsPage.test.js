import { FeedStatus } from "../../../../shared/domain/feed.js";
import { fixedClock } from "../../../../shared/infrastructure/clock.js";
import { flush, manualTimers } from "../../../../shared/testing/doubles.js";
import { mount, seededElement } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { memoryProjects } from "../../infrastructure/memory-gateway.js";
import { DIRS, summaryDTO } from "../../testing/fixtures.js";
import { REFRESH_MS, SETTLE_MS, projectsPage } from "./projectsPage.js";

file("projects/presentation/projectsPage");

const now = new Date("2026-10-07T12:00:00Z");
const pool = { id: "p1", name: "coding_pool", rootDir: "/src/coding_pool" };
const harness = { id: "p2", name: "harness", rootDir: "/src/harness" };

async function setup({ projects = [pool, harness], sessions = [], visible = true } = {}) {
  const memory = memoryProjects({ projects, dirs: DIRS, sessions });
  const summaries = await memory.gateway.listProjectSummaries();
  const el = seededElement({ summaries: summaries.map(summaryDTO) });
  const timers = manualTimers();
  const visited = [];
  const state = { visible };
  const mounted = mount(
    projectsPage({
      gateway: memory.gateway,
      navigate: (url) => visited.push(url),
      clock: fixedClock(now),
      timers,
      isVisible: () => state.visible,
    }),
    { el },
  );
  mounted.instance.init();
  mounted.tick();
  await flush();
  return { ...mounted, memory, timers, visited, state };
}

const press = (instance, key, target = { tagName: "MAIN" }) => {
  const event = { key, target, defaultPrevented: false, prevented: false, preventDefault() { this.prevented = true; } };
  instance.key(event);
  return event;
};

test("lists the seeded projects with their status and counts, the first one selected", async () => {
  const { instance } = await setup({ sessions: [{ id: "s1", projectId: "p1", ticketId: "t1", agent: "a", status: "running", lastActivityAt: now }] });
  assert.deepEqual(
    instance.rows.map((r) => [r.name, r.state, r.word, r.meta, r.href, r.settingsHref, r.selected]),
    [
      ["coding_pool", "running", "1 running", "no repos · no open tasks · active now", "/projects/p1", "/projects/p1/settings", true],
      ["harness", "idle", "idle", "no repos · no open tasks · no activity", "/projects/p2", "/projects/p2/settings", false],
    ],
  );
  assert.equal(instance.total, "2 projects · 1 session running");
  assert.equal(instance.ready, true);
});

test("the filter narrows by name or directory and keeps the cursor on a row it shows", async () => {
  const { instance } = await setup();
  instance.query = "HARN";
  instance.filtered();
  assert.deepEqual(instance.rows.map((r) => r.id), ["p2"]);
  assert.equal(instance.selectedId, "p2");
  instance.query = "nothing like it";
  assert.equal(instance.noMatch, true);
  instance.clearFilter();
  assert.equal(instance.rows.length, 2);
});

test("keys move, open, open settings and start a new project; typing in a field is left alone", async () => {
  const { instance, visited } = await setup();
  assert.ok(press(instance, "j").prevented);
  assert.equal(instance.selectedId, "p2");
  press(instance, "j");
  assert.equal(instance.selectedId, "p2", "the cursor stops at the end");
  press(instance, "ArrowUp");
  assert.equal(instance.selectedId, "p1");
  press(instance, "Enter");
  press(instance, "s");
  press(instance, "n");
  assert.deepEqual(visited, ["/projects/p1", "/projects/p1/settings", "/projects/new"]);

  assert.equal(press(instance, "j", { tagName: "INPUT" }).prevented, false);
  assert.equal(press(instance, "Enter", { tagName: "A" }).prevented, false, "a focused link opens itself");
  assert.equal(visited.length, 3);
});

test("a project created elsewhere arrives sorted and fresh; a rename re-sorts; a delete moves the cursor", async () => {
  const { instance, memory } = await setup();
  const created = await memory.gateway.createProject({ name: "alpha", rootDir: "/src" });
  assert.deepEqual(instance.rows.map((r) => r.name), ["alpha", "coding_pool", "harness"]);
  assert.equal(instance.rows[0].fresh, true);

  await memory.gateway.updateProject({ projectId: "p1", name: "zeta" });
  assert.deepEqual(instance.rows.map((r) => r.name), ["alpha", "harness", "zeta"]);

  instance.select("p2");
  await memory.gateway.deleteProject("p2");
  assert.deepEqual(instance.rows.map((r) => r.id), [created.id, "p1"]);
  assert.equal(instance.selectedId, "p1", "the next row takes the cursor");
});

test("counts are fetched a moment after a change, again every 30 s, and not while the tab is hidden", async () => {
  const { instance, memory, timers, state } = await setup();
  assert.deepEqual(timers.delays(), [REFRESH_MS]);

  await memory.gateway.addRepository({ projectId: "p2", name: "api", description: "", url: "u", rootDir: "/src/harness/api" });
  await memory.gateway.addIgnoredPath("p2", "dist"); // a catalog change
  assert.deepEqual(timers.delays(), [SETTLE_MS], "one settle timer replaces the periodic one");
  timers.tick();
  await flush();
  assert.equal(instance.rows.find((r) => r.id === "p2").meta.startsWith("1 repo"), true);
  assert.deepEqual(timers.delays(), [REFRESH_MS]);

  state.visible = false;
  await memory.gateway.addRepository({ projectId: "p2", name: "web", description: "", url: "u", rootDir: "/src/harness/web" });
  timers.tick();
  await flush();
  assert.equal(instance.rows.find((r) => r.id === "p2").meta.startsWith("1 repo"), true, "hidden: no fetch");

  state.visible = true;
  instance.onVisibility();
  assert.deepEqual(timers.delays(), [0]);
  timers.tick();
  await flush();
  assert.equal(instance.rows.find((r) => r.id === "p2").meta.startsWith("2 repos"), true);
});

test("the feed's status reaches the stream bar; coming back after a drop fetches counts at once", async () => {
  const { instance, memory, timers, dispatched } = await setup();
  assert.deepEqual(dispatched.map((d) => d.detail), [FeedStatus.LIVE]);
  memory.reportStatus(FeedStatus.RESYNCED);
  assert.deepEqual(timers.delays(), [0]);
  instance.destroy();
  assert.deepEqual(timers.delays(), []);
});

test("a malformed seed shows a coded error instead of the list", () => {
  const memory = memoryProjects();
  const { instance } = mount(projectsPage({ gateway: memory.gateway, navigate: () => {}, clock: fixedClock(now), timers: manualTimers() }), {
    el: seededElement({ summaries: null }),
  });
  instance.init();
  assert.equal(instance.error.code, "BAD_RESPONSE");
});
