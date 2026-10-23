import { assert, file, test } from "../../../shared/testing/test.js";
import { makeTask } from "../testing/fixtures.js";
import { applyTaskChange, byStatus, describeChange, dropMove, moveChoices, onBoard, RAIL_ORDER } from "./board.js";
import { STATUSES } from "./task.js";

file("tasks/domain/board");

test("the rail's order is a permutation of the one status list", () => {
  assert.deepEqual([...RAIL_ORDER].sort(), STATUSES.map((s) => s.value).sort());
  assert.deepEqual(RAIL_ORDER, ["in_progress", "review", "todo", "backlog", "done"]);
});

test("a task change replaces in place, appends when new, removes when deleted", () => {
  let tasks = [makeTask({ id: "t1" }), makeTask({ id: "t2", title: "Write docs", status: "todo" })];
  tasks = applyTaskChange(tasks, { kind: "task-upsert", task: makeTask({ id: "t1", status: "review" }) });
  assert.deepEqual(tasks.map((t) => `${t.id}:${t.status}`), ["t1:review", "t2:todo"], "a move keeps the list position");
  tasks = applyTaskChange(tasks, { kind: "task-upsert", task: makeTask({ id: "t3", title: "New", status: "backlog" }) });
  assert.deepEqual(tasks.map((t) => t.id), ["t1", "t2", "t3"]);
  tasks = applyTaskChange(tasks, { kind: "task-deleted", id: "t2" });
  assert.deepEqual(tasks.map((t) => t.id), ["t1", "t3"]);
  tasks = applyTaskChange(tasks, { kind: "task-deleted", id: "ghost" });
  assert.deepEqual(tasks.map((t) => t.id), ["t1", "t3"], "deleting an unknown task changes nothing");
});

test("byStatus has every column, even empty ones, each in list order when nothing tells them apart", () => {
  const cols = byStatus([makeTask({ id: "b", status: "done" }), makeTask({ id: "a", status: "in_progress" }), makeTask({ id: "c", status: "in_progress" })]);
  assert.deepEqual(Object.keys(cols), ["backlog", "todo", "in_progress", "review", "done"]);
  assert.deepEqual(cols.in_progress.map((t) => t.id), ["a", "c"]);
  assert.deepEqual(cols.todo, []);
});

test("each column puts the task that entered it last on top; ties keep list order, an unknown time goes last", () => {
  const at = (iso) => Date.parse(iso);
  const cols = byStatus([
    makeTask({ id: "old", status: "review", statusSince: at("2026-10-17T10:00:00Z") }),
    makeTask({ id: "unknown", status: "review", statusSince: 0 }),
    makeTask({ id: "new", status: "review", statusSince: at("2026-10-19T10:00:00Z") }),
    makeTask({ id: "tie-1", status: "review", statusSince: at("2026-10-18T10:00:00Z") }),
    makeTask({ id: "tie-2", status: "review", statusSince: at("2026-10-18T10:00:00Z") }),
  ]);
  assert.deepEqual(cols.review.map((t) => t.id), ["new", "tie-1", "tie-2", "old", "unknown"]);
});

test("a change reads as what happened to the task", () => {
  const t = makeTask();
  assert.equal(describeChange(t, { kind: "task-upsert", task: { ...t, status: "review" } }), "Add SSE feed moved to Review");
  assert.equal(describeChange(null, { kind: "task-upsert", task: { ...t, status: "todo" } }), "Add SSE feed was added to Todo");
  assert.equal(describeChange(t, { kind: "task-deleted", id: t.id }), "Add SSE feed was removed");
  assert.equal(describeChange(t, { kind: "task-upsert", task: { ...t, title: "Add the SSE feed" } }), "Add SSE feed was renamed to Add the SSE feed");
  assert.equal(describeChange(t, { kind: "task-upsert", task: { ...t, description: "longer" } }), "");
  assert.equal(describeChange(null, { kind: "task-deleted", id: "ghost" }), "");
});

test("the Move to… menu lists every status with its key, the current one checked, and opens on the next step", () => {
  const review = moveChoices("review");
  assert.deepEqual(review.map((c) => `${c.key}:${c.status}:${c.label}:${c.current}:${c.initial}`), [
    "1:backlog:Backlog:false:false",
    "2:todo:Todo:false:false",
    "3:in_progress:In progress:false:false",
    "4:review:Review:true:false",
    "5:done:Done:false:true",
  ]);
  const initial = (status) => moveChoices(status).find((c) => c.initial).status;
  assert.deepEqual(["backlog", "todo", "in_progress", "done"].map(initial), ["todo", "in_progress", "review", "review"], "Done opens on Review, so a stray Enter stays reversible");
});

test("an archived task leaves the board and the rail; restored, it comes back", () => {
  let tasks = [makeTask({ id: "t1", status: "done" }), makeTask({ id: "t2", status: "todo" })];
  tasks = applyTaskChange(tasks, { kind: "task-upsert", task: makeTask({ id: "t1", status: "done", archivedAt: 1 }) });
  assert.deepEqual(tasks.map((t) => t.id), ["t2"]);
  tasks = applyTaskChange(tasks, { kind: "task-upsert", task: makeTask({ id: "t1", status: "done", archivedAt: 1 }) });
  assert.deepEqual(tasks.map((t) => t.id), ["t2"], "the same archive again changes nothing");
  tasks = applyTaskChange(tasks, { kind: "task-upsert", task: makeTask({ id: "t1", status: "done" }) });
  assert.deepEqual(tasks.map((t) => `${t.id}:${t.status}`), ["t2:todo", "t1:done"]);
});

test("a list the server sent keeps only the tasks on the board", () => {
  assert.deepEqual(onBoard([makeTask({ id: "a" }), makeTask({ id: "b", status: "done", archivedAt: 5 })]).map((t) => t.id), ["a"]);
});

test("an archive reads as such", () => {
  const done = makeTask({ status: "done" });
  assert.equal(describeChange(done, { kind: "task-upsert", task: { ...done, archivedAt: 1 } }), "Add SSE feed was archived");
  assert.equal(describeChange(null, { kind: "task-upsert", task: { ...done, archivedAt: 1 } }), "", "a task archived before this page knew it is no news");
});

test("a drop on another column is a move there; on its own column, outside any column, or of a card mid-move, nothing", () => {
  const task = makeTask({ status: "review" });
  assert.deepEqual(dropMove(task, "done"), { taskId: "t1", status: "done" });
  assert.equal(dropMove(task, "review"), null, "its own column: no request");
  assert.equal(dropMove(task, ""), null, "outside any column: cancelled");
  assert.equal(dropMove(task, "someday"), null, "not a column");
  assert.equal(dropMove(null, "done"), null, "the card is gone");
  assert.equal(dropMove(task, "done", { pending: true }), null, "a card with a move in flight does not move again");
});
