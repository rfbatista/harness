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

test("a successful move is confirmed once; the feed's echo of it is neither washed nor announced again (Review Focus 5)", async () => {
  const { instance, store } = setup();
  await instance.moveTo(change("t1", "review"));
  assert.equal(instance.announcement, "Add SSE feed moved to Review", "the person hears their own move land");
  instance.announcement = "";
  instance.taskChanged(changed({ kind: "task-upsert", task: makeTask({ status: "review" }) }, makeTask()));
  assert.deepEqual([instance.columns[3].cards[0].fresh, instance.announcement], [false, ""]);
  // Someone else moving it afterwards is news again.
  store.tasks = [makeTask({ status: "done" })];
  instance.taskChanged(changed({ kind: "task-upsert", task: makeTask({ status: "done" }) }, makeTask({ status: "review" })));
  assert.equal(instance.announcement, "Add SSE feed moved to Done");
});

/** A gateway whose moves the test settles by hand, so echoes and responses can be interleaved. */
function heldMoves(memory) {
  const held = [];
  memory.gateway.moveTask = (id, status) => new Promise((resolve, reject) => held.push({ id, status, resolve, reject }));
  return {
    held,
    /** Settles the oldest unsettled move with the server's answer. */
    answer: (status) => held.shift().resolve(makeTask({ status })),
    fail: (err) => held.shift().reject(err),
  };
}

/** The rail applied a feed change to the store; now the board hears of it. */
function feedChange(instance, store, status, previousStatus) {
  store.tasks = [makeTask({ status })];
  instance.taskChanged(changed({ kind: "task-upsert", task: makeTask({ status }) }, makeTask({ status: previousStatus })));
}

test("the echo arriving before the response is still the board's own move", async () => {
  const { instance, store, memory } = setup();
  const server = heldMoves(memory);
  const p = instance.moveTo(change("t1", "review"));
  feedChange(instance, store, "review", "in_progress");
  assert.deepEqual([instance.columns[3].cards[0].fresh, instance.announcement], [false, ""]);
  server.answer("review");
  await p;
  assert.deepEqual([store.tasks[0].status, instance.error], ["review", null]);
});

test("an older move's echo while a newer move is pending keeps the newest status on the card and announces nothing (Review Focus 2)", async () => {
  const { instance, store, memory } = setup();
  const server = heldMoves(memory);
  const first = instance.moveTo(change("t1", "review"));
  const second = instance.moveTo(change("t1", "done"));
  feedChange(instance, store, "review", "in_progress"); // the server applied the first move and published it before answering
  assert.deepEqual([store.tasks[0].status, instance.announcement, instance.freshIds], ["done", "", []], "the card stays where the person put it last");
  server.answer("review");
  await first;
  assert.equal(store.tasks[0].status, "done", "the first response does not snap the card back");
  feedChange(instance, store, "done", "review");
  assert.deepEqual([instance.announcement, instance.freshIds], ["", []]);
  server.answer("done");
  await second;
  assert.deepEqual([store.tasks[0].status, instance.error], ["done", null]);
  assert.equal(instance.announcement, "Add SSE feed moved to Done", "the person hears the move land, once");
});

test("when both quick moves fail the card goes back to the status the server last confirmed, with an error", async () => {
  const { instance, store, memory } = setup();
  const server = heldMoves(memory);
  const first = instance.moveTo(change("t1", "review"));
  const second = instance.moveTo(change("t1", "done"));
  server.fail(new StructuredError(Codes.NETWORK, "The harness server is not reachable."));
  await first;
  assert.equal(store.tasks[0].status, "done", "the newer move is still pending");
  server.fail(new StructuredError(Codes.NETWORK, "The harness server is not reachable."));
  await second;
  assert.deepEqual([store.tasks[0].status, instance.error.code], ["in_progress", Codes.NETWORK]);
});

test("a change by someone else during a move is kept, and the move's failure is still shown", async () => {
  const { instance, store, memory } = setup();
  const server = heldMoves(memory);
  const p = instance.moveTo(change("t1", "review"));
  feedChange(instance, store, "done", "review"); // an agent moved it meanwhile
  assert.equal(instance.announcement, "Add SSE feed moved to Done");
  server.fail(new StructuredError(Codes.INVALID_INPUT, "invalid ticket status", 400));
  await p;
  assert.deepEqual([store.tasks[0].status, instance.error.code], ["done", Codes.INVALID_INPUT]);
});

test("a resync that brought a different status during a failed move is kept", async () => {
  const { instance, store, memory } = setup();
  const server = heldMoves(memory);
  const p = instance.moveTo(change("t1", "review"));
  store.tasks = [makeTask({ status: "todo" })]; // the rail resynced: the server holds todo
  server.fail(new StructuredError(Codes.NETWORK, "The harness server is not reachable."));
  await p;
  assert.deepEqual([store.tasks[0].status, instance.error.code], ["todo", Codes.NETWORK]);
});

test("after a move the card's select keeps the focus, in its new column", async () => {
  const { instance, tick, el, store } = setup();
  // A board with the card's select rendered, as the live template would.
  const render = () => {
    el.querySelectorAll(".card").forEach((c) => c.remove());
    for (const t of store.tasks) {
      const card = document.createElement("article");
      card.className = "card";
      card.dataset.taskId = t.id;
      card.dataset.status = t.status;
      const select = document.createElement("select");
      select.dataset.taskId = t.id;
      for (const status of ["in_progress", "review"]) select.append(Object.assign(document.createElement("option"), { value: status }));
      select.value = t.status;
      card.append(select);
      el.append(card);
    }
  };
  render();
  document.body.append(el);
  const select = el.querySelector("select");
  select.focus();
  assert.equal(document.activeElement, select);
  select.value = "review";
  const p = instance.moveTo({ target: select });
  render(); // Alpine re-rendered: a new select in the new column
  tick();
  assert.equal(document.activeElement?.closest(".card")?.dataset.status, "review");
  await p;
  el.remove();
});
