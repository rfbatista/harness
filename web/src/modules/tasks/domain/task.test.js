import { assert, file, test } from "../../../shared/testing/test.js";
import { deleteBlocker, isStatus, STATUSES, titleProblem } from "./task.js";

file("tasks/domain/task");

test("the statuses are the board's, in order", () => {
  assert.deepEqual(STATUSES.map((s) => s.value), ["backlog", "todo", "in_progress", "review", "done"]);
  assert.ok(isStatus("review"));
  assert.ok(!isStatus("someday"));
});

test("a task needs a title", () => {
  assert.ok(titleProblem("  "));
  assert.equal(titleProblem("Write docs"), "");
});

test("a task with sessions is not deleted: they would be left under no task", () => {
  assert.equal(deleteBlocker({ total: 0, live: 0 }), "");
  assert.ok(deleteBlocker({ total: 2, live: 1 }).includes("1 running session"));
  assert.ok(deleteBlocker({ total: 3, live: 0 }).includes("3 sessions"));
});
