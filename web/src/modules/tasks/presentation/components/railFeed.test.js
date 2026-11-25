import { FeedStatus } from "../../../../shared/domain/feed.js";
import { mount, seededElement } from "../../../../shared/testing/alpine.js";
import { flush } from "../../../../shared/testing/doubles.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { memoryRail } from "../../infrastructure/memory-rail.js";
import { makeTask, taskDTO } from "../../testing/fixtures.js";
import { railFeed } from "./railFeed.js";

file("tasks/presentation/railFeed");

const s = (id, ticketId, status) => ({ id, ticketId, status, pendingApprovals: 0 });

function setup({ tasks = [makeTask()], sessions = [], reportsFeed = false, archived = [] } = {}) {
  const memory = memoryRail({ projectId: "p1", sessions, tasks, archived });
  const store = { byTask: {}, tasks: [], seeded: false };
  const el = seededElement({ project_id: "p1", sessions, tasks: tasks.map(taskDTO), archived_count: archived.length }, "rail-seed");
  if (reportsFeed) el.dataset.reportsFeed = "";
  const feed = mount(railFeed({ gateway: memory.gateway, store }), { el });
  feed.instance.init();
  return { memory, store, feed };
}

test("it seeds the shared store from the page's seed and draws nothing", () => {
  const { store } = setup();
  assert.deepEqual([store.seeded, store.projectId, store.tasks.map((t) => t.id)], [true, "p1", ["t1"]]);
});

test("a malformed seed leaves the store unseeded", () => {
  const store = { byTask: {}, tasks: [], seeded: false };
  const feed = mount(railFeed({ gateway: memoryRail().gateway, store }), { el: seededElement({ project_id: "p1" }, "rail-seed") });
  feed.instance.init();
  assert.equal(store.seeded, false);
});

test("a ticket change moves the store's task and is dispatched with what it was before", async () => {
  const { memory, store, feed } = setup();
  await flush();
  const moved = makeTask({ status: "review" });
  memory.emit({ kind: "task-upsert", task: moved });
  assert.equal(store.tasks[0].status, "review");
  assert.deepEqual(feed.dispatched.filter((d) => d.name === "task-changed").at(-1), { name: "task-changed", detail: { change: { kind: "task-upsert", task: moved }, previous: makeTask() } });
  memory.emit({ kind: "task-upsert", task: makeTask({ id: "t2", title: "New", status: "backlog" }) });
  assert.deepEqual(feed.dispatched.filter((d) => d.name === "task-changed").at(-1).detail.previous, null);
  memory.emit({ kind: "task-deleted", id: "t1" });
  assert.deepEqual(store.tasks.map((t) => t.id), ["t2"]);
});

test("a session started on a task shows in the store's activity", async () => {
  const { memory, store } = setup();
  await flush();
  memory.emit({ kind: "upsert", session: s("b", "t1", "idle") });
  assert.deepEqual(store.byTask.t1, { live: 1, attention: true });
  memory.emit({ kind: "deleted", id: "b" });
  assert.equal(store.byTask.t1, undefined);
});

test("it dispatches feed-change for every change and reports the status only on the page's feed", async () => {
  const quiet = setup();
  await flush();
  quiet.memory.emit({ kind: "task-upsert", task: makeTask({ title: "Renamed" }) });
  assert.deepEqual(quiet.feed.dispatched.map((d) => d.name), ["feed-change", "task-changed"]);
  const { memory, feed } = setup({ reportsFeed: true });
  await flush();
  memory.status(FeedStatus.PAUSED);
  assert.deepEqual(feed.dispatched.filter((d) => d.name === "feed-status").map((d) => d.detail), [FeedStatus.LIVE, FeedStatus.PAUSED]);
});

test("a status move is kept in the store and is not a task change", async () => {
  const { memory, store, feed } = setup();
  await flush();
  const change = { taskId: "t1", status: "review", reason: "", by: "person", bySessionId: "", at: null };
  memory.emit({ kind: "task-status", change });
  assert.deepEqual([store.statusChanges.t1, feed.dispatched.some((d) => d.name === "task-changed")], [change, false]);
});

test("after a resync it reads the sessions and the tasks again", async () => {
  const { memory, store } = setup({ sessions: [s("a", "t1", "running")] });
  await flush();
  memory.replace([s("a", "t1", "done"), s("c", "t9", "idle")]);
  memory.replaceTasks([makeTask({ id: "t9", title: "Elsewhere", status: "done" })]);
  memory.status(FeedStatus.RESYNCED);
  await flush();
  assert.deepEqual(store.tasks.map((t) => t.id), ["t9"]);
  assert.deepEqual(store.byTask.t9, { live: 1, attention: true });
});

test("when the feed first goes live it reads the tasks again, only that once", async () => {
  const { memory, store } = setup({ tasks: [makeTask({ status: "review" })] });
  memory.replaceTasks([makeTask({ status: "review", mergeState: { state: "not_merged", merged: 0, total: 1 } })]);
  await flush();
  await flush();
  assert.deepEqual(store.tasks.map((t) => t.mergeState?.state ?? null), ["not_merged"]);
  memory.replaceTasks([makeTask({ status: "review", mergeState: { state: "merged", merged: 1, total: 1 } })]);
  memory.status(FeedStatus.LIVE);
  await flush();
  assert.equal(store.tasks[0].mergeState.state, "not_merged", "only the first live reads again");
});

test("a resync still refreshes the sessions when the tasks cannot be read, and the other way round", async () => {
  const { memory, store } = setup({ tasks: [makeTask(), makeTask({ id: "t3", title: "Third", status: "todo" })] });
  await flush();
  memory.gateway.listTasks = async () => {
    throw new Error("list_tickets is down");
  };
  memory.replace([s("c", "t3", "running")]);
  memory.status(FeedStatus.RESYNCED);
  await flush();
  assert.equal(store.byTask.t3.live, 1, "sessions refreshed");
  assert.deepEqual(store.tasks.map((t) => t.id), ["t1", "t3"], "tasks kept as they were");
});

test("an archived task leaves the store at once and the archived count goes up; a restored one comes back and the count is read again", async () => {
  const done = makeTask({ id: "t-done", title: "Ship it", status: "done" });
  const { memory, store } = setup({ tasks: [makeTask(), done], archived: [makeTask({ id: "t-old", status: "done", archivedAt: 1 })] });
  await flush();
  assert.equal(store.archivedCount, 1);
  memory.emit({ kind: "task-upsert", task: { ...done, archivedAt: 5 } });
  assert.deepEqual([store.tasks.map((t) => t.id), store.archivedCount], [["t1"], 2]);
  memory.emit({ kind: "task-upsert", task: { ...done, archivedAt: 5 } });
  assert.equal(store.archivedCount, 2, "the same archive again is not counted twice");
  memory.emit({ kind: "task-upsert", task: done });
  await flush();
  assert.deepEqual([store.tasks.map((t) => t.id), store.archivedCount], [["t1", "t-done"], 1]);
});

test("a seed or a resync that carries an archived task keeps it off the store", async () => {
  const { memory, store } = setup({ tasks: [makeTask(), makeTask({ id: "t-gone", status: "done", archivedAt: 1 })] });
  assert.deepEqual(store.tasks.map((t) => t.id), ["t1"]);
  memory.replaceTasks([makeTask(), makeTask({ id: "t-gone", status: "done", archivedAt: 1 })]);
  memory.status(FeedStatus.RESYNCED);
  await flush();
  assert.deepEqual(store.tasks.map((t) => t.id), ["t1"]);
});

test("a restored feature brings back the children archived on their own before it, which the feed does not carry", async () => {
  const feature = makeTask({ id: "f", title: "Files tab", status: "done" });
  const kid = makeTask({ id: "k", title: "Notes pane", status: "done", parentId: "f", archivedAt: 3 });
  const { memory, store } = setup({ tasks: [makeTask()] });
  await flush();
  memory.replaceTasks([makeTask(), feature, kid]);
  memory.emit({ kind: "task-upsert", task: feature });
  await flush();
  assert.deepEqual(store.tasks.map((t) => `${t.id}:${Boolean(t.archivedAt)}`).sort(), ["f:false", "k:true", "t1:false"]);
});
