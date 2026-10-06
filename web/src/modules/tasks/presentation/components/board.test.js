import { Codes, StructuredError } from "../../../../shared/domain/errors.js";
import { mount } from "../../../../shared/testing/alpine.js";
import { manualTimers } from "../../../../shared/testing/doubles.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { memoryTasks } from "../../infrastructure/memory-gateway.js";
import { makeTask } from "../../testing/fixtures.js";
import { board, FRESH_MS } from "./board.js";

file("tasks/presentation/board");

function setup({ tasks = [makeTask()], seeded = true } = {}) {
  const store = { byTask: {}, tasks, seeded };
  const memory = memoryTasks({ projects: ["p1"], tasks });
  const el = document.createElement("main");
  el.dataset.projectId = "p1";
  const ssr = document.createElement("div");
  ssr.dataset.ssr = "";
  el.append(ssr);
  const timers = manualTimers();
  const mounted = mount(board({ gateway: memory.gateway, store, setTimeout: timers.setTimeout }), { el });
  mounted.instance.init();
  return { ...mounted, store, el, memory, timers };
}

/** The change event a card's select fires. */
const change = (taskId, value) => ({ target: { dataset: { taskId }, value } });

/** The task-changed event tasksRail dispatches after applying a feed change. */
const changed = (change, previous) => ({ detail: { change, previous } });

test("renders the store's tasks as columns once seeded, and drops the server's copy", () => {
  const { instance, tick, el } = setup();
  assert.equal(instance.hasTasks, true);
  assert.equal(instance.isEmpty, false);
  assert.deepEqual(instance.columns.map((c) => c.cards.map((t) => t.title)), [[], [], ["Add SSE feed"], [], []]);
  assert.equal(instance.ready, false);
  tick();
  assert.equal(instance.ready, true);
  assert.equal(el.querySelector("[data-ssr]"), null);
});

test("the empty state shows when the last task goes, and the board when the first arrives", () => {
  const { instance, store } = setup({ tasks: [] });
  assert.deepEqual([instance.hasTasks, instance.isEmpty], [false, true]);
  store.tasks = [makeTask()];
  assert.deepEqual([instance.hasTasks, instance.isEmpty], [true, false]);
});

test("without a seed it leaves the server's rendering alone", () => {
  const { instance, tick, el } = setup({ seeded: false });
  tick();
  assert.deepEqual([instance.hasTasks, instance.isEmpty, instance.ready], [false, false, false]);
  assert.ok(el.querySelector("[data-ssr]"));
});

test("moving a card is optimistic, then settles on the server's task", async () => {
  const { instance, store, memory } = setup();
  const p = instance.moveTo(change("t1", "review"));
  assert.equal(store.tasks[0].status, "review", "moved before the server answers");
  await p;
  assert.deepEqual(memory.calls.at(-1), { updateTask: { id: "t1", status: "review" } });
  assert.equal(memory.tasks()[0].status, "review");
  assert.equal(instance.error, null);
});

test("choosing the status it already has sends nothing", async () => {
  const { instance, memory } = setup();
  await instance.moveTo(change("t1", "in_progress"));
  assert.deepEqual(memory.calls, []);
});

test("a refused move goes back and shows the coded error", async () => {
  const { instance, store, memory } = setup();
  memory.refuse = () => new StructuredError(Codes.INVALID_INPUT, "invalid ticket status", 400);
  await instance.moveTo(change("t1", "review"));
  assert.equal(store.tasks[0].status, "in_progress");
  assert.deepEqual([instance.error.code, instance.error.next], [Codes.INVALID_INPUT, "fix the form and try again"]);
  instance.dismissError();
  assert.equal(instance.error, null);
});

test("a card deleted meanwhile disappears instead of going back (Review Focus 3)", async () => {
  const { instance, store, memory } = setup();
  await memory.gateway.deleteTask("t1");
  await instance.moveTo(change("t1", "review"));
  assert.deepEqual(store.tasks, []);
  assert.equal(instance.error.code, Codes.TICKET_NOT_FOUND);
});

test("two quick moves: the later one wins and the first response does not snap back (Review Focus 2)", async () => {
  const { instance, store } = setup();
  const first = instance.moveTo(change("t1", "review"));
  const second = instance.moveTo(change("t1", "done"));
  await Promise.all([first, second]);
  assert.equal(store.tasks[0].status, "done");
});

test("a resync during a move does not leave the moved card stale (Review Focus 4)", async () => {
  const { instance, store } = setup();
  const p = instance.moveTo(change("t1", "review"));
  store.tasks = [makeTask({ status: "in_progress" })]; // the rail resynced from the server, which has not applied the move yet
  await p;
  assert.equal(store.tasks[0].status, "review", "the response is applied on top of the resync");
});

test("a task moved elsewhere is washed for a while and announced", () => {
  const { instance, store, timers } = setup();
  const moved = makeTask({ status: "review" });
  store.tasks = [moved]; // the rail applied it
  instance.taskChanged(changed({ kind: "task-upsert", task: moved }, makeTask()));
  assert.equal(instance.columns[3].cards[0].fresh, true);
  assert.equal(instance.announcement, "Add SSE feed moved to Review");
  assert.deepEqual(timers.delays(), [FRESH_MS]);
  timers.tick();
  assert.equal(instance.columns[3].cards[0].fresh, false);
});

test("a task created elsewhere appears washed; a deleted one is announced as removed", () => {
  const { instance, store } = setup({ tasks: [] });
  const created = makeTask({ id: "t2", title: "New", status: "backlog" });
  store.tasks = [created];
  instance.taskChanged(changed({ kind: "task-upsert", task: created }, null));
  assert.deepEqual([instance.columns[0].cards[0].fresh, instance.announcement], [true, "New was added to Backlog"]);
  store.tasks = [];
  instance.taskChanged(changed({ kind: "task-deleted", id: "t2" }, created));
  assert.equal(instance.announcement, "New was removed");
});

test("the feed's echo of this board's own move is neither washed nor announced (Review Focus 5)", async () => {
  const { instance, store } = setup();
  await instance.moveTo(change("t1", "review"));
  instance.taskChanged(changed({ kind: "task-upsert", task: makeTask({ status: "review" }) }, makeTask()));
  assert.deepEqual([instance.columns[3].cards[0].fresh, instance.announcement], [false, ""]);
  // Someone else moving it afterwards is news again.
  store.tasks = [makeTask({ status: "done" })];
  instance.taskChanged(changed({ kind: "task-upsert", task: makeTask({ status: "done" }) }, makeTask({ status: "review" })));
  assert.equal(instance.announcement, "Add SSE feed moved to Done");
});
