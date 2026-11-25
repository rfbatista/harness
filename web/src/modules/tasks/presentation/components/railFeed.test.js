import { FeedStatus } from "../../../../shared/domain/feed.js";
import { mount, seededElement } from "../../../../shared/testing/alpine.js";
import { flush } from "../../../../shared/testing/doubles.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { memoryRail } from "../../infrastructure/memory-rail.js";
import { makeTask, taskDTO } from "../../testing/fixtures.js";
import { railFeed } from "./railFeed.js";

file("tasks/presentation/railFeed");

function setup({ tasks = [makeTask()], sessions = [], reportsFeed = false } = {}) {
  const memory = memoryRail({ projectId: "p1", sessions, tasks });
  const store = { byTask: {}, tasks: [], seeded: false, drawerOpen: false };
  const el = seededElement({ project_id: "p1", sessions, tasks: tasks.map(taskDTO), archived_count: 0 }, "rail-seed");
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
  const store = { byTask: {}, tasks: [], seeded: false, drawerOpen: false };
  const feed = mount(railFeed({ gateway: memoryRail().gateway, store }), { el: seededElement({ project_id: "p1" }, "rail-seed") });
  feed.instance.init();
  assert.equal(store.seeded, false);
});

test("it tells an observer about a task or session change, before and after the store takes it", async () => {
  const { memory, store } = setup();
  await flush();
  const seen = [];
  store.observer = {
    before: (change) => seen.push(["before", change.kind, store.tasks[0].status]),
    after: (change, previous) => seen.push(["after", change.kind, store.tasks[0].status, previous?.status ?? null]),
  };
  memory.emit({ kind: "task-upsert", task: makeTask({ status: "review" }) });
  assert.deepEqual(seen, [["before", "task-upsert", "in_progress"], ["after", "task-upsert", "review", "in_progress"]]);
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
