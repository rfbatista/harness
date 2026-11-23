import { assert, file, test } from "../../../shared/testing/test.js";
import { makeTask } from "../testing/fixtures.js";
import { afterClear, archivedSummary, clearableOf, clearScopeIds, toArchivedRows, toClearStrip } from "./archivedView.js";

file("tasks/presentation/archivedView");

const at = (y, m, d) => new Date(y, m - 1, d, 12).getTime();

test("rows are newest archived first, each with its finish and archive dates, its merge badge and its link", () => {
  const rows = toArchivedRows(
    [
      makeTask({ id: "old", title: "Old", status: "done", statusSince: at(2026, 9, 1), archivedAt: at(2026, 9, 2) }),
      makeTask({ id: "new", title: "New", status: "done", statusSince: at(2026, 10, 17), archivedAt: at(2026, 10, 18), mergeState: { state: "merged", merged: 1, total: 1 } }),
    ],
    { projectId: "p1", receipts: {}, busy: {}, confirming: "", errors: {} },
  );
  assert.deepEqual(rows.map((r) => r.id), ["new", "old"]);
  const [r] = rows;
  assert.deepEqual([r.href, r.finished, r.archived, r.hasMerge, r.mergeState, r.mergeWord], ["/projects/p1/tasks/new", "Finished Oct 17", "Archived Oct 18", true, "merged", "Merged"]);
  assert.deepEqual([r.receipt, r.busy, r.confirming, r.error], ["", false, false, null]);
  assert.equal(rows[1].hasMerge, false);
});

test("a restored or deleted row collapses to its receipt; a busy row says so; one row asks before deleting", () => {
  const rows = toArchivedRows(
    [makeTask({ id: "a", title: "Ship it", status: "done", archivedAt: 3 }), makeTask({ id: "b", title: "Docs", status: "done", archivedAt: 2 }), makeTask({ id: "c", title: "Feed", status: "done", archivedAt: 1 })],
    { projectId: "p1", receipts: { a: "restored", b: "deleted" }, busy: { c: "restoring" }, confirming: "c", errors: {} },
  );
  assert.deepEqual(rows.map((r) => r.receipt), ["Ship it restored to Done.", "Deleted Docs.", ""]);
  assert.deepEqual([rows[0].restoredReceipt, rows[1].restoredReceipt], [true, false]);
  assert.deepEqual([rows[2].busy, rows[2].busyText, rows[2].confirming, rows[2].confirmTitle], [true, "Restoring…", true, "Delete Feed forever?"]);
});

test("the summary counts the rows still archived, and says what Restore and Delete do", () => {
  assert.equal(archivedSummary(23), "23 archived tasks. Restore puts a task back in Done. Delete removes it and everything it holds.");
  assert.equal(archivedSummary(1), "1 archived task. Restore puts a task back in Done. Delete removes it and everything it holds.");
  assert.equal(archivedSummary(0), "");
});

test("a row with no worktree on record, its feature's children included, has nothing to clear", () => {
  const tasks = [makeTask({ id: "f", title: "Feature", status: "done", archivedAt: 3 }), makeTask({ id: "k", title: "Kid", status: "done", parentId: "f", archivedAt: 3 }), makeTask({ id: "x", title: "X", status: "done", archivedAt: 2 })];
  const worktrees = { k: 2, x: 0 };
  assert.deepEqual([clearableOf(tasks, worktrees, "f"), clearableOf(tasks, worktrees, "k"), clearableOf(tasks, worktrees, "x")], [2, 2, 0]);
  const rows = toArchivedRows(tasks, { projectId: "p1", receipts: {}, busy: {}, confirming: "", errors: {}, worktrees });
  assert.deepEqual(rows.map((r) => [r.id, r.noWorktrees]), [["f", false], ["k", false], ["x", true]]);
  assert.equal(rows[2].clearTitle, "Its worktrees are already cleared");
  assert.deepEqual(clearScopeIds(tasks, "f"), ["f", "k"]);
});

test("after a clear a task keeps one worktree per session the clear skipped", () => {
  assert.deepEqual(afterClear({ a: 2, b: 1, c: 4 }, ["a", "b"], [{ task_id: "a" }]), { a: 1, b: 0, c: 4 });
});

test("the confirm states the dirty count before it is confirmed, and what is skipped", () => {
  const preview = { worktrees: 7, dirty: 2, running: 1 };
  const ready = toClearStrip({ label: "all archived tasks", phase: "ready", preview, result: null, error: null });
  assert.equal(ready.title, "Clear 7 worktrees of all archived tasks?");
  assert.deepEqual(ready.lines.slice(0, 2), ["2 hold uncommitted changes that will be lost.", "1 running session will be skipped."]);
  assert.equal(ready.canConfirm, true);
  const one = toClearStrip({ label: "Ship it", phase: "ready", preview: { worktrees: 1, dirty: 0, running: 0 }, result: null, error: null });
  assert.deepEqual([one.title, one.lines[0]], ["Clear 1 worktree of Ship it?", "None hold uncommitted changes."]);
  assert.equal(toClearStrip({ label: "Ship it", phase: "loading", preview: null, result: null, error: null }).canConfirm, false);
  assert.equal(toClearStrip({ label: "Ship it", phase: "clearing", preview: one && { worktrees: 1, dirty: 0, running: 0 }, result: null, error: null }).busy, true);
});

test("the confirm says how many sessions stay because another task's session shares their worktree", () => {
  const ready = toClearStrip({ label: "Ship it", phase: "ready", preview: { worktrees: 2, dirty: 0, running: 0, shared: 3 }, result: null, error: null });
  assert.ok(ready.lines.includes("3 sessions share a worktree with a task that is not being cleared; their worktrees stay."), JSON.stringify(ready.lines));
  const none = toClearStrip({ label: "Ship it", phase: "ready", preview: { worktrees: 0, dirty: 0, running: 0, shared: 1 }, result: null, error: null });
  assert.equal(none.title, "Nothing to clear in Ship it");
  assert.ok(none.lines[0].includes("1 session shares a worktree with a task that is not being cleared"), none.lines[0]);
});

test("a preview with nothing to remove says so instead of offering an empty confirm", () => {
  const none = toClearStrip({ label: "Ship it", phase: "ready", preview: { worktrees: 0, dirty: 0, running: 2 }, result: null, error: null });
  assert.deepEqual([none.title, none.canConfirm, none.closes], ["Nothing to clear in Ship it", false, true]);
  assert.match(none.lines[0], /2 running sessions keep its worktree until it ends/);
});

test("the result says how many were cleared and which were skipped and why", () => {
  const done = toClearStrip({
    label: "x",
    phase: "done",
    preview: null,
    result: { cleared: 6, skipped: [{ task_title: "Feed", reason: "running" }, { task_title: "Docs", reason: "failed", message: "device busy" }] },
    error: null,
  });
  assert.equal(done.title, "Cleared 6 worktrees");
  assert.deepEqual(done.lines, ["Feed: still running, so its worktree stays.", "Docs: could not be removed: device busy."]);
});

test("a shared worktree and an unreadable one are skipped with their own words, never as an unknown error", () => {
  const done = toClearStrip({
    label: "x",
    phase: "done",
    preview: null,
    result: { cleared: 1, skipped: [{ task_title: "Feed", reason: "shared" }, { task_title: "Docs", reason: "error", message: "the store is down" }] },
    error: null,
  });
  assert.deepEqual(done.lines, [
    "Feed: its worktree is shared with a task that is not being cleared, so it stays.",
    "Docs: could not tell who is on its worktree (the store is down), so it stays.",
  ]);
  assert.ok(!done.lines.join(" ").includes("unknown error"));
});
