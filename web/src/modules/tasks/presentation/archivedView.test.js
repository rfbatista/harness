import { assert, file, test } from "../../../shared/testing/test.js";
import { makeTask } from "../testing/fixtures.js";
import { archivedSummary, toArchivedRows } from "./archivedView.js";

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
