import { FeedStatus } from "../../../../shared/domain/feed.js";
import { mount, seededElement } from "../../../../shared/testing/alpine.js";
import { flush } from "../../../../shared/testing/doubles.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { memoryRail } from "../../infrastructure/memory-rail.js";
import { makeTask, taskDTO } from "../../testing/fixtures.js";
import { rail } from "./rail.js";

file("tasks/presentation/rail");

const s = (id, ticketId, status, pendingApprovals = 0) => ({ id, ticketId, status, pendingApprovals });

function setup({ tasks = [makeTask()], currentTask = "t1", reportsFeed = false, sessions = [s("a", "t1", "running")] } = {}) {
  const memory = memoryRail({ projectId: "p1", sessions, tasks });
  const store = { byTask: {}, tasks: [], seeded: false };
  const el = seededElement({ project_id: "p1", sessions, tasks: tasks.map(taskDTO) }, "rail-seed");
  el.dataset.currentTask = currentTask;
  if (reportsFeed) el.dataset.reportsFeed = "";
  const nav = mount(rail({ gateway: memory.gateway, store }), { el });
  nav.instance.init();
  /** The link of a task, wherever its group is now. */
  const link = (taskId) => nav.instance.groups.flatMap((g) => g.links).find((l) => l.id === taskId);
  return { memory, store, nav, link };
}

test("groups come from the store in the rail's order, the open task marked current", () => {
  const { nav } = setup({ tasks: [makeTask({ id: "t-done", title: "Ship it", status: "done" }), makeTask(), makeTask({ id: "t2", title: "Write docs", status: "todo" })] });
  assert.deepEqual(nav.instance.groups.map((g) => `${g.label}: ${g.links.map((l) => l.label).join(", ")}`), ["in progress: Add SSE feed", "todo: Write docs", "done: Ship it"]);
  const [feed] = nav.instance.groups[0].links;
  assert.deepEqual([feed.href, feed.current, feed.state, feed.word, feed.hasDot, feed.live, feed.hasCount], ["/projects/p1/tasks/t1", "page", "running", "running", true, 1, true]);
  assert.deepEqual([nav.instance.groups[1].links[0].current, nav.instance.groups[1].links[0].hasDot], [false, false]);
});

test("a ticket change regroups the rail; the last task gone leaves it empty", async () => {
  const { memory, nav } = setup();
  await flush();
  assert.equal(nav.instance.isEmpty, false);
  memory.emit({ kind: "task-upsert", task: makeTask({ status: "review" }) });
  assert.deepEqual(nav.instance.groups.map((g) => g.label), ["review"]);
  memory.emit({ kind: "task-deleted", id: "t1" });
  assert.deepEqual([nav.instance.groups, nav.instance.isEmpty], [[], true]);
});

test("a session started on a task shows on its link as it starts", async () => {
  const { memory, link } = setup({ tasks: [makeTask(), makeTask({ id: "t2", title: "Write docs", status: "todo" })] });
  await flush();
  assert.deepEqual([link("t2").hasDot, link("t2").hasCount], [false, false]);
  memory.emit({ kind: "upsert", session: s("b", "t2", "running") });
  assert.deepEqual([link("t2").live, link("t2").state, link("t2").word], [1, "running", "running"]);
  memory.emit({ kind: "upsert", session: s("b", "t2", "idle") });
  assert.deepEqual([link("t2").state, link("t2").word], ["waiting", "waiting on you"]);
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
  assert.deepEqual(nav.dispatched.at(-1), { name: "task-changed", detail: { change: { kind: "task-upsert", task: moved }, previous: makeTask() } });
  memory.emit({ kind: "task-upsert", task: makeTask({ id: "t2", title: "New", status: "backlog" }) });
  assert.deepEqual(nav.dispatched.at(-1).detail.previous, null);
  memory.emit({ kind: "task-deleted", id: "t1" });
  assert.deepEqual(store.tasks.map((t) => t.id), ["t2"]);
});

test("after a resync it reads the sessions and the tasks again", async () => {
  const { memory, store, link } = setup();
  await flush();
  memory.replace([s("a", "t1", "done"), s("c", "t9", "running")]);
  memory.replaceTasks([makeTask({ id: "t9", title: "Elsewhere", status: "done" })]);
  memory.status(FeedStatus.RESYNCED);
  await flush();
  assert.deepEqual(store.tasks.map((t) => t.id), ["t9"]);
  assert.equal(link("t9").live, 1);
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
  assert.equal(link("t3").live, 1, "sessions refreshed");
  assert.deepEqual(store.tasks.map((t) => t.id), ["t1", "t3"], "tasks kept as they were");
});

test("a malformed seed leaves the store unseeded and the server's rendering alone", () => {
  const memory = memoryRail();
  const store = { byTask: {}, tasks: [], seeded: false };
  const nav = mount(rail({ gateway: memory.gateway, store }), { el: seededElement({ project_id: "p1" }, "rail-seed") });
  nav.instance.init();
  nav.tick();
  assert.deepEqual([store.seeded, nav.instance.groups, nav.instance.isEmpty], [false, [], false]);
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
