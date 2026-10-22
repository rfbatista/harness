import { assert, file, test } from "../../../shared/testing/test.js";
import { makeTask } from "../testing/fixtures.js";
import { toChildRows } from "./featureView.js";

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
