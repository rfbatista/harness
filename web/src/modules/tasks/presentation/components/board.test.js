import { Codes, StructuredError } from "../../../../shared/domain/errors.js";
import { mount } from "../../../../shared/testing/alpine.js";
import { manualTimers } from "../../../../shared/testing/doubles.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { memoryTasks } from "../../infrastructure/memory-gateway.js";
import { makeTask } from "../../testing/fixtures.js";
import { fixedClock } from "../../../../shared/infrastructure/clock.js";
import { AGE_TICK_MS, board, BUMP_MS, FAILED_MS, FRESH_MS } from "./board.js";

file("tasks/presentation/board");

function setup({ tasks = [makeTask()], seeded = true, clock = fixedClock("2026-10-19T12:00:00Z"), fine = true, narrow = false } = {}) {
  const store = { byTask: {}, tasks, seeded };
  const memory = memoryTasks({ projects: ["p1"], tasks });
  const el = document.createElement("main");
  el.dataset.projectId = "p1";
  const ssr = document.createElement("div");
  ssr.dataset.ssr = "";
  el.append(ssr);
  const timers = manualTimers();
  const mounted = mount(board({ gateway: memory.gateway, store, setTimeout: timers.setTimeout, clock, isFine: () => fine, isNarrow: () => narrow }), { el });
  mounted.instance.init();
  return { ...mounted, store, el, memory, timers, clock };
}

/** A card's menu wrapper, the target of its menu-select. */
function cardMenu(taskId) {
  const card = document.createElement("article");
  card.dataset.taskId = taskId;
  const menu = document.createElement("div");
  card.append(menu);
  return menu;
}

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
  const p = instance.move("t1", "review");
  assert.equal(store.tasks[0].status, "review", "moved before the server answers");
  await p;
  assert.deepEqual(memory.calls.at(-1), { updateTask: { id: "t1", status: "review" } });
  assert.equal(memory.tasks()[0].status, "review");
  assert.equal(instance.error, null);
});

test("choosing the status it already has sends nothing", async () => {
  const { instance, memory } = setup();
  await instance.move("t1", "in_progress");
  assert.deepEqual(memory.calls, []);
});

test("a refused move goes back and shows the coded error", async () => {
  const { instance, store, memory } = setup();
  memory.refuse = () => new StructuredError(Codes.INVALID_INPUT, "invalid ticket status", 400);
  await instance.move("t1", "review");
  assert.equal(store.tasks[0].status, "in_progress");
  assert.deepEqual([instance.error.code, instance.error.next], [Codes.INVALID_INPUT, "fix the form and try again"]);
  instance.dismissError();
  assert.equal(instance.error, null);
});

test("a card deleted meanwhile disappears instead of going back (Review Focus 3)", async () => {
  const { instance, store, memory } = setup();
  await memory.gateway.deleteTask("t1");
  await instance.move("t1", "review");
  assert.deepEqual(store.tasks, []);
  assert.equal(instance.error.code, Codes.TICKET_NOT_FOUND);
});

test("two quick moves: the later one wins and the first response does not snap back (Review Focus 2)", async () => {
  const { instance, store } = setup();
  const first = instance.move("t1", "review");
  const second = instance.move("t1", "done");
  await Promise.all([first, second]);
  assert.equal(store.tasks[0].status, "done");
});

test("a resync during a move does not leave the moved card stale (Review Focus 4)", async () => {
  const { instance, store } = setup();
  const p = instance.move("t1", "review");
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
  assert.deepEqual(timers.delays(), [AGE_TICK_MS, FRESH_MS]);
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
  await instance.move("t1", "review");
  assert.equal(instance.announcement, "Moved Add SSE feed to Review.", "the person hears their own move land");
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
  const p = instance.move("t1", "review");
  feedChange(instance, store, "review", "in_progress");
  assert.deepEqual([instance.columns[3].cards[0].fresh, instance.announcement], [false, ""]);
  server.answer("review");
  await p;
  assert.deepEqual([store.tasks[0].status, instance.error], ["review", null]);
});

test("an older move's echo while a newer move is pending keeps the newest status on the card and announces nothing (Review Focus 2)", async () => {
  const { instance, store, memory } = setup();
  const server = heldMoves(memory);
  const first = instance.move("t1", "review");
  const second = instance.move("t1", "done");
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
  assert.equal(instance.announcement, "Moved Add SSE feed to Done.", "the person hears the move land, once");
});

test("when both quick moves fail the card goes back to the status the server last confirmed, with an error", async () => {
  const { instance, store, memory } = setup();
  const server = heldMoves(memory);
  const first = instance.move("t1", "review");
  const second = instance.move("t1", "done");
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
  const p = instance.move("t1", "review");
  feedChange(instance, store, "done", "review"); // an agent moved it meanwhile
  assert.equal(instance.announcement, "Add SSE feed moved to Done");
  server.fail(new StructuredError(Codes.INVALID_INPUT, "invalid ticket status", 400));
  await p;
  assert.deepEqual([store.tasks[0].status, instance.error.code], ["done", Codes.INVALID_INPUT]);
});

test("a resync that brought a different status during a failed move is kept", async () => {
  const { instance, store, memory } = setup();
  const server = heldMoves(memory);
  const p = instance.move("t1", "review");
  store.tasks = [makeTask({ status: "todo" })]; // the rail resynced: the server holds todo
  server.fail(new StructuredError(Codes.NETWORK, "The harness server is not reachable."));
  await p;
  assert.deepEqual([store.tasks[0].status, instance.error.code], ["todo", Codes.NETWORK]);
});

test("a menu choice moves its card, and the focus stays on the card's Move to…, in its new column", async () => {
  const { instance, tick, el, store } = setup();
  // A board with each card's menu button rendered, as the live template would.
  const render = () => {
    el.querySelectorAll(".card").forEach((c) => c.remove());
    for (const t of store.tasks) {
      const card = document.createElement("article");
      card.className = "card";
      card.dataset.taskId = t.id;
      card.dataset.status = t.status;
      card.innerHTML = `<div class="menu"><button type="button" class="move-to">Move to…</button></div>`;
      el.append(card);
    }
  };
  render();
  document.body.append(el);
  const menu = el.querySelector(".menu");
  menu.querySelector("button").focus();
  const p = instance.menuSelect({ target: menu, detail: { action: "move", value: "review" } });
  render(); // Alpine re-rendered: a new card in the new column
  tick();
  assert.equal(store.tasks[0].status, "review");
  assert.equal(document.activeElement?.closest(".card")?.dataset.status, "review");
  assert.ok(document.activeElement?.classList.contains("move-to"));
  await p;
  el.remove();
});

test("a moved card sits on top of its new column, pending until the server answers", async () => {
  const older = makeTask({ id: "old", status: "review", statusSince: Date.parse("2026-10-19T11:00:00Z") });
  const { instance, memory } = setup({ tasks: [makeTask({ statusSince: Date.parse("2026-10-18T11:00:00Z") }), older] });
  const server = heldMoves(memory);
  const p = instance.move("t1", "review");
  const [top] = instance.columns[3].cards;
  assert.deepEqual([top.id, top.pending, top.note], ["t1", true, "Moving to Review…"]);
  server.answer("review");
  await p;
  assert.equal(instance.columns[3].cards.find((c) => c.id === "t1").pending, false);
});

test("a refused move puts the card back where it was, says the move failed for a while, and announces it", async () => {
  const since = Date.parse("2026-10-18T11:00:00Z");
  const { instance, memory, timers } = setup({ tasks: [makeTask({ statusSince: since })] });
  memory.refuse = () => new StructuredError(Codes.INVALID_INPUT, "invalid ticket status", 400);
  await instance.move("t1", "done");
  const [card] = instance.columns[2].cards;
  assert.deepEqual([card.id, card.failed, card.note, card.pending], ["t1", true, "Move failed", false]);
  assert.equal(instance.announcement, "Couldn't move Add SSE feed to Done.");
  assert.ok(timers.delays().includes(FAILED_MS));
  timers.tick();
  assert.equal(instance.columns[2].cards[0].failed, false);
});

test("each card's time in column moves on once a minute, without reordering the column", () => {
  const since = (iso) => Date.parse(iso);
  const { instance, timers, clock } = setup({
    tasks: [makeTask({ id: "a", statusSince: since("2026-10-19T11:58:00Z") }), makeTask({ id: "b", statusSince: since("2026-10-19T11:50:00Z") })],
  });
  assert.deepEqual(instance.columns[2].cards.map((c) => `${c.id}:${c.age}`), ["a:2m", "b:10m"]);
  assert.ok(timers.delays().includes(AGE_TICK_MS));
  clock.set("2026-10-19T12:05:00Z");
  timers.tick();
  assert.deepEqual(instance.columns[2].cards.map((c) => `${c.id}:${c.age}`), ["a:7m", "b:15m"]);
});

test("Archive takes a done card off the board at once; the server confirms it, the count goes up and it is announced", async () => {
  const done = makeTask({ id: "t-done", title: "Ship it", status: "done" });
  const { instance, store, memory } = setup({ tasks: [makeTask(), done] });
  store.archivedCount = 3;
  const p = instance.menuSelect({ target: cardMenu("t-done"), detail: { action: "archive" } });
  assert.deepEqual(store.tasks.map((t) => t.id), ["t1"], "gone before the server answers");
  await p;
  assert.ok(memory.tasks().find((t) => t.id === "t-done").archivedAt > 0);
  assert.deepEqual([store.archivedCount, instance.archivedLink, instance.announcement, instance.error], [4, "Archived (4)", "Archived Ship it.", null]);
  // The feed's echo: the rail found nothing to remove and tells the board.
  instance.taskChanged(changed({ kind: "task-upsert", task: { ...done, archivedAt: 9 } }, null));
  assert.deepEqual([instance.announcement, instance.freshIds], ["Archived Ship it.", []]);
});

test("a refused archive brings the card back where it was, with the error", async () => {
  const done = makeTask({ id: "t-done", title: "Ship it", status: "done" });
  const { instance, store, memory } = setup({ tasks: [done, makeTask()] });
  memory.gateway.archiveTask = async () => {
    throw new StructuredError(Codes.TASK_NOT_DONE, "only a done task can be archived", 409);
  };
  await instance.archive("t-done");
  assert.deepEqual(store.tasks.map((t) => t.id), ["t-done", "t1"]);
  assert.deepEqual([instance.error.code, instance.announcement], [Codes.TASK_NOT_DONE, "Couldn't archive Ship it."]);
});

test("someone else archiving a card takes it off and says so", () => {
  const done = makeTask({ id: "t-done", title: "Ship it", status: "done" });
  const { instance, store } = setup({ tasks: [makeTask(), done] });
  store.tasks = [makeTask()]; // the rail applied it
  instance.taskChanged(changed({ kind: "task-upsert", task: { ...done, archivedAt: 9 } }, done));
  assert.equal(instance.announcement, "Ship it was archived");
});

test("Archived (N) shows the server's count until the rail has its own", () => {
  const { instance, store, el } = setup({ seeded: false });
  el.dataset.archivedCount = "7";
  instance.init();
  assert.equal(instance.archivedLink, "Archived (7)");
  store.seeded = true;
  store.archivedCount = 8;
  assert.equal(instance.archivedLink, "Archived (8)");
  assert.equal(instance.archivedHref, "/projects/p1/archived");
});

test("a Done card's menu offers Archive; other cards' do not", () => {
  const { instance } = setup({ tasks: [makeTask({ id: "a", status: "done" }), makeTask({ id: "b", status: "review" })] });
  assert.deepEqual([instance.columns[4].cards[0].canArchive, instance.columns[3].cards[0].canArchive], [true, false]);
});

const doneTasks = (n) => Array.from({ length: n }, (_, i) => makeTask({ id: `d${i}`, title: `Done ${i}`, status: "done" }));

test("Archive all asks first, in page; Cancel closes it and sends nothing", () => {
  const { instance, memory } = setup({ tasks: [makeTask(), ...doneTasks(3)] });
  assert.deepEqual([instance.archiveAll.shows, instance.archiveAll.open], [true, false]);
  instance.askArchiveAll();
  assert.deepEqual([instance.archiveAll.open, instance.archiveAll.confirm], [true, "Archive 3"]);
  instance.cancelArchiveAll();
  assert.equal(instance.archiveAll.open, false);
  assert.deepEqual(memory.calls, []);
});

test("Archive all, confirmed: one request, the cards leave, the count goes up once, the column and the live region say how many", async () => {
  const { instance, store, memory } = setup({ tasks: [makeTask(), ...doneTasks(3)] });
  store.archivedCount = 2;
  instance.askArchiveAll();
  const server = { done: null };
  const real = memory.gateway.archiveDoneTasks;
  memory.gateway.archiveDoneTasks = (pid) => new Promise((resolve) => (server.done = () => resolve(real(pid))));
  const p = instance.confirmArchiveAll();
  assert.deepEqual([instance.archiveAll.phase, instance.archiveAll.title, store.tasks.length], ["busy", "Archiving 3…", 4], "not optimistic: the cards stay, faded");
  // One echo arrives before the answer: the rail already took that card off and counted it.
  store.tasks = store.tasks.filter((t) => t.id !== "d0");
  store.archivedCount = 3;
  server.done();
  await p;
  assert.deepEqual(store.tasks.map((t) => t.id), ["t1"]);
  assert.deepEqual([store.archivedCount, instance.archiveAll.emptyText, instance.announcement], [5, "3 tasks archived.", "Archived 3 tasks."]);
});

test("Archive all refused: nothing leaves, the strip says so and can try again", async () => {
  const { instance, store, memory } = setup({ tasks: doneTasks(2) });
  memory.gateway.archiveDoneTasks = async () => {
    throw new StructuredError(Codes.NETWORK, "The harness server is not reachable.");
  };
  instance.askArchiveAll();
  await instance.confirmArchiveAll();
  assert.deepEqual([instance.archiveAll.phase, instance.archiveAll.title, store.tasks.length, instance.archiveAllError.code], ["failed", "Couldn't archive. Nothing changed.", 2, Codes.NETWORK]);
  instance.dismissArchiveAll();
  assert.deepEqual([instance.archiveAll.phase, instance.archiveAllError], ["rest", null]);
});

/** A drag event on a card or a column, as the browser fires it. */
function dragEvent(el, over = {}) {
  const data = {};
  return {
    target: el,
    clientX: 0,
    clientY: 0,
    relatedTarget: null,
    defaultPrevented: false,
    preventDefault() {
      this.defaultPrevented = true;
    },
    dataTransfer: { setData: (k, v) => (data[k] = v), effectAllowed: "", dropEffect: "" },
    ...over,
  };
}
const cardEl = (id) => {
  const card = document.createElement("article");
  card.dataset.taskId = id;
  return card;
};
const columnEl = (status) => {
  const col = document.createElement("section");
  col.dataset.column = status;
  return col;
};

test("dragging a card to another column previews it there; the drop moves it like the menu does", async () => {
  const { instance, store, memory } = setup({ tasks: [makeTask({ status: "review" })] });
  const start = dragEvent(cardEl("t1"));
  instance.dragStart(start);
  assert.deepEqual([start.defaultPrevented, instance.dragging, instance.columns[3].dragOrigin, instance.columns[3].cards[0].ghost], [false, true, true, true]);
  const over = dragEvent(columnEl("done"));
  instance.dragOver(over);
  assert.deepEqual([over.defaultPrevented, instance.columns[4].dropOver, instance.columns[4].countText], [true, true, "0 → 1"]);
  const p = instance.drop(dragEvent(columnEl("done")));
  assert.deepEqual([instance.dragging, store.tasks[0].status], [false, "done"], "optimistic, at once");
  await p;
  assert.deepEqual(memory.calls.at(-1), { updateTask: { id: "t1", status: "done" } });
  instance.dragEnd(dragEvent(cardEl("t1")));
  assert.equal(store.tasks[0].status, "done");
});

test("a drop on its own column, or a drag that ends outside any column, sends nothing", () => {
  const { instance, memory } = setup({ tasks: [makeTask({ status: "review" })] });
  instance.dragStart(dragEvent(cardEl("t1")));
  instance.dragOver(dragEvent(columnEl("review")));
  assert.deepEqual([instance.columns[3].dropSelf, instance.columns[3].selfText], [true, "Already in Review"]);
  instance.drop(dragEvent(columnEl("review")));
  instance.dragStart(dragEvent(cardEl("t1")));
  instance.dragEnd(dragEvent(cardEl("t1")));
  assert.deepEqual([memory.calls, instance.dragging, instance.announcement], [[], false, ""]);
});

test("no drag starts from the title link or the menu button, on a coarse pointer, or for a card mid-move", async () => {
  const { instance, memory } = setup({ tasks: [makeTask({ status: "review" })] });
  const link = document.createElement("a");
  cardEl("t1").append(link);
  instance.pointerDown({ target: link });
  const fromLink = dragEvent(cardEl("t1"));
  instance.dragStart(fromLink);
  assert.deepEqual([fromLink.defaultPrevented, instance.dragging], [true, false]);
  instance.pointerDown({ target: cardEl("t1") });
  memory.gateway.moveTask = () => new Promise(() => {}); // never answers
  instance.move("t1", "done");
  const midMove = dragEvent(cardEl("t1"));
  instance.dragStart(midMove);
  assert.deepEqual([midMove.defaultPrevented, instance.dragging], [true, false]);
  const coarse = setup({ tasks: [makeTask()], fine: false });
  const touch = dragEvent(cardEl("t1"));
  coarse.instance.dragStart(touch);
  assert.deepEqual([touch.defaultPrevented, coarse.instance.columns[2].cards[0].draggable], [true, false]);
});

test("the dragged card changed elsewhere cancels the drag and says so", () => {
  const { instance, store } = setup({ tasks: [makeTask({ status: "review" })] });
  instance.dragStart(dragEvent(cardEl("t1")));
  store.tasks = [makeTask({ status: "todo" })];
  instance.taskChanged(changed({ kind: "task-upsert", task: makeTask({ status: "todo" }) }, makeTask({ status: "review" })));
  assert.deepEqual([instance.dragging, instance.announcement], [false, "Add SSE feed moved to Todo"]);
});

test("on a narrow screen a menu move rings its new column's chip for a moment", async () => {
  const { instance, timers } = setup({ narrow: true });
  await instance.menuSelect({ target: cardMenu("t1"), detail: { action: "move", value: "done" } });
  assert.deepEqual(instance.chips.filter((c) => c.bumped).map((c) => c.status), ["done"]);
  assert.ok(timers.delays().includes(BUMP_MS));
  timers.tick();
  assert.deepEqual(instance.chips.filter((c) => c.bumped), []);
});

test("on a wide screen a menu move rings no chip", async () => {
  const { instance } = setup();
  await instance.menuSelect({ target: cardMenu("t1"), detail: { action: "move", value: "done" } });
  assert.deepEqual(instance.chips.filter((c) => c.bumped), []);
});
