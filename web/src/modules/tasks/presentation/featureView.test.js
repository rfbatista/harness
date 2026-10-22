import { assert, file, test } from "../../../shared/testing/test.js";
import { makeTask } from "../testing/fixtures.js";
import { featureSummary, toChildRows } from "./featureView.js";

file("tasks/presentation/featureView");

test("child rows link the child, say its status in words, and carry its painted documents in order", () => {
  const rows = toChildRows([makeTask({ id: "c 1", title: "CSV export", status: "in_progress", parentId: "f1" }), makeTask({ id: "c2", status: "todo", parentId: "f1" })], {
    projectId: "p1",
    documents: { "c 1": [{ title: "Brief: CSV", href: "/d1", updated: "2m" }, { title: "Notes", href: "/d2", updated: "1h" }] },
  });
  assert.deepEqual([rows[0].href, rows[0].statusLabel, rows[0].noDocuments], ["/projects/p1/tasks/c%201", "In progress", false]);
  assert.deepEqual(rows[0].documents.map((d) => `${d.title}:${d.after}:${d.updatedTitle}`), ["Brief: CSV:false:updated 2m", "Notes:true:updated 1h"]);
  assert.deepEqual([rows[1].statusLabel, rows[1].noDocuments, rows[1].documents], ["Todo", true, []]);
});

test("child rows come in review, in progress, todo, backlog, done order, ties in list order", () => {
  const kids = ["done", "backlog", "todo", "in_progress", "review", "todo", "review"].map((status, i) => makeTask({ id: `c${i}`, status, parentId: "f1" }));
  const rows = toChildRows(kids, { projectId: "p1", documents: {} });
  assert.deepEqual(rows.map((r) => r.id), ["c4", "c6", "c3", "c2", "c5", "c1", "c0"]);
  assert.deepEqual(rows.map((r) => r.done), [false, false, false, false, false, false, true]);
});

test("a child row counts its documents in words, and says nothing for none", () => {
  const rows = toChildRows([makeTask({ id: "a", parentId: "f1" }), makeTask({ id: "b", parentId: "f1" }), makeTask({ id: "c", parentId: "f1" })], {
    projectId: "p1",
    documents: { a: [{ title: "X", href: "/x", updated: "1m" }], b: [{ title: "X", href: "/x", updated: "1m" }, { title: "Y", href: "/y", updated: "1m" }] },
  });
  assert.deepEqual(rows.map((r) => r.docCount), ["1 doc", "2 docs", ""]);
});

const rowsOf = (...statuses) => toChildRows(statuses.map((status, i) => makeTask({ id: `c${i}`, status, parentId: "f1" })), { projectId: "p1", documents: {} });

test("the feature summary counts the children and says what needs the person or is moving", () => {
  const mixed = featureSummary(rowsOf(...Array(3).fill("review"), ...Array(2).fill("in_progress"), ...Array(11).fill("todo")));
  assert.deepEqual([mixed.count, mixed.phrase, mixed.has], ["16", "3 in review · 2 in progress", true]);
  assert.equal(featureSummary(rowsOf("review", "done")).phrase, "1 in review");
  assert.equal(featureSummary(rowsOf("in_progress", "done")).phrase, "1 in progress");
});

test("the feature summary says all done when every child is done, and nothing when none is moving", () => {
  assert.equal(featureSummary(rowsOf("done", "done")).phrase, "all done");
  assert.equal(featureSummary(rowsOf("todo", "backlog", "done")).phrase, "");
  assert.equal(featureSummary(rowsOf("todo")).count, "1");
});

test("a feature with no children has nothing for the strip to show", () => {
  const none = featureSummary([]);
  assert.deepEqual([none.has, none.count, none.phrase], [false, "0", ""]);
});
