import { FeedStatus } from "../../../../shared/domain/feed.js";
import { mount, seededElement } from "../../../../shared/testing/alpine.js";
import { flush, manualTimers } from "../../../../shared/testing/doubles.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { memoryRailPrefs } from "../../infrastructure/memory-rail-prefs.js";
import { memoryRail } from "../../infrastructure/memory-rail.js";
import { makeTask, taskDTO } from "../../testing/fixtures.js";
import { rail } from "./rail.js";

file("tasks/presentation/rail");

const s = (id, ticketId, status, pendingApprovals = 0) => ({ id, ticketId, status, pendingApprovals });

function setup({ tasks = [makeTask()], currentTask = "t1", reportsFeed = false, sessions = [s("a", "t1", "running")], collapsed = [], narrow = false } = {}) {
  const memory = memoryRail({ projectId: "p1", sessions, tasks });
  const store = { byTask: {}, tasks: [], seeded: false, drawerOpen: false };
  const prefs = memoryRailPrefs(collapsed);
  const timers = manualTimers();
  const visited = [];
  const el = seededElement({ project_id: "p1", sessions, tasks: tasks.map(taskDTO) }, "rail-seed");
  el.dataset.currentTask = currentTask;
  if (reportsFeed) el.dataset.reportsFeed = "";
  const nav = mount(rail({ gateway: memory.gateway, store, prefs, navigate: (url) => visited.push(url), timers, isNarrow: () => narrow }), { el });
  nav.instance.init();
  /** A group by key. */
  const group = (key) => nav.instance.groups.find((g) => g.key === key);
  /** The row of a task, wherever its group is now. */
  const link = (taskId) => nav.instance.groups.flatMap((g) => [...g.rows, ...g.more]).find((l) => l.id === taskId);
  return { memory, store, prefs, timers, visited, nav, group, link };
}

const key = (k, over = {}) => ({ key: k, shiftKey: false, metaKey: false, ctrlKey: false, altKey: false, defaultPrevented: false, preventDefault() { this.defaultPrevented = true; }, ...over });

test("every group comes from the store in the rail's order, the open task marked current", () => {
  const { nav, link } = setup({ tasks: [makeTask({ id: "t-done", title: "Ship it", status: "done" }), makeTask(), makeTask({ id: "t2", title: "Write docs", status: "todo" })] });
  assert.deepEqual(nav.instance.groups.map((g) => `${g.label} ${g.countText}: ${g.rows.map((l) => l.title).join(", ")}`), ["In progress 1: Add SSE feed", "Review 0: ", "Todo 1: Write docs", "Backlog 0: ", "Done 1: Ship it"]);
  const feed = link("t1");
  assert.deepEqual([feed.href, feed.current, feed.state, feed.isLive, feed.liveText], ["/projects/p1/tasks/t1", "page", "running", true, "1 live"]);
  assert.deepEqual([link("t2").current, link("t2").hasDot], [false, false]);
  assert.equal(nav.instance.total, 3);
});

test("a ticket change regroups the rail; the last task gone leaves it empty", async () => {
  const { memory, nav, group } = setup();
  await flush();
  assert.equal(nav.instance.isEmpty, false);
  memory.emit({ kind: "task-upsert", task: makeTask({ status: "review" }) });
  assert.deepEqual([group("in_progress").countText, group("review").rows.map((r) => r.id)], ["0", ["t1"]]);
  memory.emit({ kind: "task-deleted", id: "t1" });
  assert.deepEqual([nav.instance.isEmpty, nav.instance.hasTasks], [true, false]);
});

test("a session started on a task shows on its row as it starts", async () => {
  const { memory, link } = setup({ tasks: [makeTask(), makeTask({ id: "t2", title: "Write docs", status: "todo" })] });
  await flush();
  assert.deepEqual([link("t2").hasDot, link("t2").isLive], [false, false]);
  memory.emit({ kind: "upsert", session: s("b", "t2", "running") });
  assert.deepEqual([link("t2").liveText, link("t2").state], ["1 live", "running"]);
  memory.emit({ kind: "upsert", session: s("b", "t2", "idle") });
  assert.deepEqual([link("t2").state, link("t2").isWaiting, link("t2").attention], ["waiting", true, true]);
  memory.emit({ kind: "deleted", id: "b" });
  assert.equal(link("t2").hasDot, false);
});

test("the server copy goes once the live groups have rendered", () => {
  const { nav } = setup();
  const ssr = document.createElement("div");
  ssr.dataset.ssr = "";
  nav.instance.$el.append(ssr);
  nav.tick();
  assert.equal(nav.instance.$el.querySelector("[data-ssr]"), null);
  assert.equal(nav.instance.live, true);
});

test("the store starts from the seed's tasks", () => {
  const { store } = setup();
  assert.deepEqual([store.seeded, store.tasks.map((t) => t.id)], [true, ["t1"]]);
});

test("a ticket change moves the store's task and is dispatched with what it was before", async () => {
  const { memory, store, nav } = setup();
  await flush();
  const moved = makeTask({ status: "review" });
  memory.emit({ kind: "task-upsert", task: moved });
  assert.equal(store.tasks[0].status, "review");
  assert.deepEqual(nav.dispatched.filter((d) => d.name === "task-changed").at(-1), { name: "task-changed", detail: { change: { kind: "task-upsert", task: moved }, previous: makeTask() } });
  memory.emit({ kind: "task-upsert", task: makeTask({ id: "t2", title: "New", status: "backlog" }) });
  assert.deepEqual(nav.dispatched.filter((d) => d.name === "task-changed").at(-1).detail.previous, null);
  memory.emit({ kind: "task-deleted", id: "t1" });
  assert.deepEqual(store.tasks.map((t) => t.id), ["t2"]);
});

test("after a resync it reads the sessions and the tasks again, quietly", async () => {
  const { memory, store, link, nav } = setup();
  await flush();
  memory.replace([s("a", "t1", "done"), s("c", "t9", "idle")]);
  memory.replaceTasks([makeTask({ id: "t9", title: "Elsewhere", status: "done" })]);
  memory.status(FeedStatus.RESYNCED);
  await flush();
  assert.deepEqual(store.tasks.map((t) => t.id), ["t9"]);
  assert.deepEqual([link("t9").liveText, link("t9").fresh, link("t9").signal], ["1 live", false, false]);
  assert.equal(nav.dispatched.some((d) => d.name === "rail-announce"), false, "a resync is not news");
});

test("a resync still refreshes the sessions when the tasks cannot be read, and the other way round", async () => {
  const { memory, store, link } = setup({ tasks: [makeTask(), makeTask({ id: "t3", title: "Third", status: "todo" })] });
  await flush();
  memory.gateway.listTasks = async () => {
    throw new Error("list_tickets is down");
  };
  memory.replace([s("a", "t1", "done"), s("c", "t3", "running")]);
  memory.status(FeedStatus.RESYNCED);
  await flush();
  assert.equal(link("t3").liveText, "1 live", "sessions refreshed");
  assert.deepEqual(store.tasks.map((t) => t.id), ["t1", "t3"], "tasks kept as they were");
});

test("a malformed seed leaves the store unseeded and the server's rendering alone", () => {
  const memory = memoryRail();
  const store = { byTask: {}, tasks: [], seeded: false, drawerOpen: false };
  const nav = mount(rail({ gateway: memory.gateway, store, prefs: memoryRailPrefs(), navigate: () => {}, timers: manualTimers() }), { el: seededElement({ project_id: "p1" }, "rail-seed") });
  nav.instance.init();
  nav.tick();
  assert.deepEqual([store.seeded, nav.instance.live, nav.instance.isEmpty], [false, false, false]);
});

test("it reports the feed's status to the stream bar only where it is the page's feed", async () => {
  const quiet = setup();
  await flush();
  assert.equal(quiet.nav.dispatched.some((d) => d.name === "feed-status"), false, "the task page's own feed reports there");
  const { memory, nav } = setup({ reportsFeed: true });
  await flush();
  memory.status(FeedStatus.PAUSED);
  assert.deepEqual(nav.dispatched.filter((d) => d.name === "feed-status").map((d) => d.detail), [FeedStatus.LIVE, FeedStatus.PAUSED]);
});

test("Review requests counts what waits on the person across the project, live", async () => {
  const { memory, nav, link } = setup({ tasks: [makeTask({ id: "t1", pendingReviews: 2 }), makeTask({ id: "t2", status: "todo" })], sessions: [] });
  assert.deepEqual([nav.instance.reviewsCount, nav.instance.hasReviews], [2, true]);
  assert.deepEqual([link("t1").state, link("t1").reviewsText], ["waiting", "2 reviews"]);
  memory.emit({ kind: "task-upsert", task: makeTask({ id: "t1", pendingReviews: 0 }) });
  assert.deepEqual([nav.instance.reviewsCount, nav.instance.hasReviews, link("t1").hasDot], [0, false, false]);
});

test("folding a group is remembered, and the rail starts folded as the server painted it", () => {
  const tasks = [makeTask({ id: "b1", status: "backlog" }), makeTask({ id: "d1", status: "done" })];
  const { nav, prefs, group } = setup({ tasks, collapsed: ["done", "review"] });
  assert.deepEqual([group("done").collapsed, group("done").expanded, group("review").collapsed], [true, false, false]);
  nav.instance.toggleGroup({ currentTarget: { dataset: { key: "backlog" } } });
  assert.deepEqual([group("backlog").collapsed, prefs.collapsed()], [true, ["done", "review", "backlog"]]);
  nav.instance.toggleGroup({ currentTarget: { dataset: { key: "done" } } });
  assert.deepEqual([group("done").collapsed, prefs.collapsed()], [false, ["review", "backlog"]]);
  assert.equal(nav.instance.turned, true, "the chevron turns once a click folded something");
});

test("the filter keeps matching tasks, says how many 500ms after typing stops, and Esc clears it", () => {
  const tasks = [makeTask({ id: "t1", title: "Redesign sidebar" }), makeTask({ id: "t2", title: "Write docs", status: "todo" })];
  const { nav, timers, group } = setup({ tasks });
  nav.instance.query = "side";
  nav.instance.filtered();
  assert.deepEqual([group("in_progress").countText, group("todo").countText, group("todo").foldable], ["1 of 1", "0 of 1", false]);
  assert.deepEqual(timers.delays(), [500]);
  timers.tick();
  assert.equal(nav.instance.filterResult, "1 task matches");
  const esc = key("Escape");
  nav.instance.filterKey(esc);
  assert.deepEqual([nav.instance.query, nav.instance.filterResult, esc.defaultPrevented, group("todo").countText], ["", "", true, "1"]);
  const again = key("Escape");
  nav.instance.filterKey(again);
  assert.equal(again.defaultPrevented, false, "a second Esc does nothing");
});

test("no match at all says so; Enter opens the first match", () => {
  const { nav, visited, timers } = setup({ tasks: [makeTask({ id: "t1", title: "Redesign sidebar" }), makeTask({ id: "t2", title: "Sidebar keys", status: "todo" })] });
  nav.instance.query = "zzz";
  nav.instance.filtered();
  timers.tick();
  assert.deepEqual([nav.instance.hasNoMatch, nav.instance.noMatchText, nav.instance.filterResult], [true, "No task matches “zzz”.", "No task matches"]);
  nav.instance.query = "sidebar";
  nav.instance.filterKey(key("Enter"));
  assert.deepEqual(visited, ["/projects/p1/tasks/t1"]);
  nav.instance.query = "";
  nav.instance.filterKey(key("Enter"));
  assert.equal(visited.length, 1, "Enter with no text opens nothing");
});

test("Needs you shows only what waits on the person, and stays on when nothing is left", async () => {
  const tasks = [makeTask({ id: "t1" }), makeTask({ id: "t2", status: "todo", pendingReviews: 1 })];
  const { memory, nav, group } = setup({ tasks, sessions: [] });
  assert.deepEqual([nav.instance.needsYouCount, nav.instance.showsNeedsYou, nav.instance.needsYouOnly], [1, true, false]);
  nav.instance.toggleNeedsYou();
  assert.deepEqual([group("in_progress").countText, group("todo").rows.map((r) => r.id)], ["0 of 1", ["t2"]]);
  memory.emit({ kind: "task-upsert", task: makeTask({ id: "t2", status: "todo", pendingReviews: 0 }) });
  assert.deepEqual([nav.instance.needsYouCount, nav.instance.needsYouOnly, nav.instance.showsNeedsYou, nav.instance.nothingNeedsYou], [0, true, true, true]);
  nav.instance.toggleNeedsYou();
  assert.deepEqual([nav.instance.showsNeedsYou, nav.instance.nothingNeedsYou], [false, false]);
});

test("a task that starts needing the person rings its dot once and is announced, several at once in one sentence", async () => {
  const tasks = [makeTask({ id: "t1", title: "Fix flaky test" }), makeTask({ id: "t2", title: "Write docs", status: "todo" }), makeTask({ id: "t3", title: "Port", status: "todo" })];
  const { memory, nav, link, timers } = setup({ tasks, sessions: [] });
  await flush();
  memory.emit({ kind: "upsert", session: s("a", "t1", "idle") });
  assert.equal(link("t1").signal, true);
  timers.tick();
  assert.deepEqual(nav.dispatched.filter((d) => d.name === "rail-announce").map((d) => d.detail), ["“Fix flaky test” is waiting on you."]);
  assert.equal(link("t1").signal, false, "the ring is one-shot");
  memory.emit({ kind: "upsert", session: s("b", "t2", "waiting_approval") });
  memory.emit({ kind: "task-upsert", task: makeTask({ id: "t3", title: "Port", status: "todo", pendingReviews: 1 }) });
  timers.tick();
  assert.deepEqual(nav.dispatched.filter((d) => d.name === "rail-announce").map((d) => d.detail).at(-1), "2 tasks are waiting on you.");
  memory.emit({ kind: "deleted", id: "a" });
  timers.tick();
  assert.equal(nav.dispatched.filter((d) => d.name === "rail-announce").length, 2, "nothing is said when a task stops waiting");
});

test("a task that arrives or moves takes the arrival wash; a rename does not", async () => {
  const { memory, link, timers } = setup();
  await flush();
  memory.emit({ kind: "task-upsert", task: makeTask({ id: "t2", title: "New", status: "todo" }) });
  memory.emit({ kind: "task-upsert", task: makeTask({ status: "review" }) });
  assert.deepEqual([link("t2").fresh, link("t1").fresh], [true, true]);
  timers.tick();
  memory.emit({ kind: "task-upsert", task: makeTask({ status: "review", title: "Renamed" }) });
  assert.deepEqual([link("t2").fresh, link("t1").fresh], [false, false]);
});

test("the Tasks button opens the drawer; Esc closes it", () => {
  const { nav, store } = setup();
  nav.instance.openDrawer();
  assert.equal(store.drawerOpen, true);
  nav.instance.drawerKey(key("Escape"));
  assert.equal(store.drawerOpen, false);
});

test("/ focuses the filter, but not from a text field or the terminal, and opens the drawer on a phone", () => {
  const { nav, store } = setup({ narrow: true });
  const input = document.createElement("textarea");
  const terminal = document.createElement("div");
  terminal.className = "xterm";
  const inTerminal = document.createElement("span");
  terminal.append(inTerminal);
  for (const target of [input, inTerminal]) {
    const e = key("/", { target });
    nav.instance.globalKey(e);
    assert.equal(e.defaultPrevented, false);
  }
  const e = key("/", { target: document.body });
  nav.instance.globalKey(e);
  assert.deepEqual([e.defaultPrevented, store.drawerOpen], [true, true]);
});
