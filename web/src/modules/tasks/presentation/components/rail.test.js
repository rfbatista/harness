import { FeedStatus } from "../../../../shared/domain/feed.js";
import { mount, seededElement } from "../../../../shared/testing/alpine.js";
import { flush } from "../../../../shared/testing/doubles.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { memoryRail } from "../../infrastructure/memory-rail.js";
import { makeTask, taskDTO } from "../../testing/fixtures.js";
import { rail, railLink } from "./rail.js";

file("tasks/presentation/rail");

const s = (id, ticketId, status, pendingApprovals = 0) => ({ id, ticketId, status, pendingApprovals });

function setup({ tasks = [makeTask()] } = {}) {
  const seeded = [s("a", "t1", "running")];
  const memory = memoryRail({ projectId: "p1", sessions: seeded, tasks });
  const store = { byTask: {}, tasks: [], seeded: false };
  const nav = mount(rail({ gateway: memory.gateway, store }), {
    el: seededElement({ project_id: "p1", sessions: seeded, tasks: tasks.map(taskDTO) }, "rail-seed"),
  });
  nav.instance.init();
  const link = (taskId) => {
    const el = document.createElement("a");
    el.dataset.taskId = taskId;
    const m = mount(railLink({ store }), { el });
    m.instance.init();
    return m.instance;
  };
  return { memory, store, nav, link };
}

test("links start from the seed", () => {
  const { link } = setup();
  const t1 = link("t1");
  assert.deepEqual([t1.live, t1.hasCount, t1.dotState], [1, true, "running"]);
  const t2 = link("t2");
  assert.deepEqual([t2.hasCount, t2.hasDot], [false, false]);
});

test("a session started on a task shows on its link as it starts", async () => {
  const { memory, link } = setup();
  const t2 = link("t2");
  await flush();
  memory.emit({ kind: "upsert", session: s("b", "t2", "running") });
  assert.deepEqual([t2.live, t2.dotState, t2.dotWord], [1, "running", "running"]);
  memory.emit({ kind: "upsert", session: s("b", "t2", "idle") });
  assert.deepEqual([t2.dotState, t2.dotWord], ["waiting", "waiting on you"]);
  memory.emit({ kind: "deleted", id: "b" });
  assert.equal(t2.hasDot, false);
});

test("after a resync it reads the sessions again", async () => {
  const { memory, link } = setup();
  const t1 = link("t1");
  await flush();
  memory.replace([s("a", "t1", "done"), s("c", "t3", "running")]);
  memory.status(FeedStatus.RESYNCED);
  await flush();
  assert.equal(t1.hasCount, false);
  assert.equal(link("t3").live, 1);
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

test("after a resync it reads the tasks again too", async () => {
  const { memory, store } = setup();
  await flush();
  memory.replaceTasks([makeTask({ id: "t9", title: "Elsewhere", status: "done" })]);
  memory.status(FeedStatus.RESYNCED);
  await flush();
  assert.deepEqual(store.tasks.map((t) => t.id), ["t9"]);
});

test("a malformed seed leaves the store unseeded and the server's rendering alone", () => {
  const memory = memoryRail();
  const store = { byTask: {}, tasks: [], seeded: false };
  const nav = mount(rail({ gateway: memory.gateway, store }), { el: seededElement({ project_id: "p1" }, "rail-seed") });
  nav.instance.init();
  assert.equal(store.seeded, false);
});

test("it reports the feed's status to the stream bar only where it is the page's feed", async () => {
  const quiet = setup();
  await flush();
  assert.equal(quiet.nav.dispatched.some((d) => d.name === "feed-status"), false, "the task page's own feed reports there");
  const seeded = [s("a", "t1", "running")];
  const memory = memoryRail({ projectId: "p1", sessions: seeded, tasks: [makeTask()] });
  const el = seededElement({ project_id: "p1", sessions: seeded, tasks: [taskDTO(makeTask())] }, "rail-seed");
  el.dataset.reportsFeed = "";
  const nav = mount(rail({ gateway: memory.gateway, store: { byTask: {}, tasks: [], seeded: false } }), { el });
  nav.instance.init();
  await flush();
  memory.status(FeedStatus.PAUSED);
  assert.deepEqual(nav.dispatched.filter((d) => d.name === "feed-status").map((d) => d.detail), [FeedStatus.LIVE, FeedStatus.PAUSED]);
});

test("a resync still refreshes the sessions when the tasks cannot be read, and the other way round", async () => {
  const { memory, store, link } = setup();
  await flush();
  memory.gateway.listTasks = async () => {
    throw new Error("list_tickets is down");
  };
  memory.replace([s("a", "t1", "done"), s("c", "t3", "running")]);
  memory.status(FeedStatus.RESYNCED);
  await flush();
  assert.equal(link("t3").live, 1, "sessions refreshed");
  assert.deepEqual(store.tasks.map((t) => t.id), ["t1"], "tasks kept as they were");
});
