import { mount, seededElement } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { makeTask } from "../../testing/fixtures.js";
import { feature } from "./feature.js";

file("tasks/presentation/feature");

function setup({ tasks, seeded = true, documents = {}, painted = 0 } = {}) {
  const store = { byTask: {}, tasks, seeded };
  const el = seededElement({ documents }, "feature-seed");
  el.dataset.taskId = "f1";
  el.dataset.projectId = "p1";
  el.dataset.children = String(painted);
  const ssr = el.ownerDocument.createElement("div");
  ssr.dataset.ssr = "";
  el.append(ssr);
  const mounted = mount(feature({ store }), { el });
  mounted.instance.init();
  return { ...mounted, store, el };
}

const featureTask = makeTask({ id: "f1", title: "Exports" });

test("renders the children from the store with their painted documents, and drops the server copy", () => {
  const { instance, store, el, tick } = setup({
    tasks: [featureTask, makeTask({ id: "c1", title: "CSV export", status: "todo", parentId: "f1" }), makeTask({ id: "t9" })],
    documents: { c1: [{ title: "Brief: CSV", href: "/projects/p1/tasks/c1/documents/d1", updated: "2m" }] },
  });
  assert.equal(instance.hasChildren, true);
  assert.equal(instance.count, "1");
  assert.deepEqual(instance.children.map((c) => `${c.id}:${c.statusLabel}:${c.documents.length}`), ["c1:Todo:1"]);
  tick();
  assert.equal(el.querySelector("[data-ssr]"), null);
  assert.equal(instance.ready, true);
  store.tasks = [...store.tasks]; // untouched store: same rows
  assert.equal(instance.children.length, 1);
});

test("a child that arrives over the feed appears with no documents yet; a move changes its status; a removal drops it", () => {
  const { instance, store } = setup({ tasks: [featureTask] });
  assert.equal(instance.hasChildren, false);
  store.tasks = [...store.tasks, makeTask({ id: "c2", title: "PDF export", status: "backlog", parentId: "f1" })];
  assert.equal(instance.hasChildren, true);
  assert.deepEqual([instance.children[0].title, instance.children[0].noDocuments], ["PDF export", true]);
  store.tasks = store.tasks.map((t) => (t.id === "c2" ? { ...t, status: "done" } : t));
  assert.equal(instance.children[0].statusLabel, "Done");
  store.tasks = store.tasks.filter((t) => t.id !== "c2");
  assert.equal(instance.hasChildren, false);
  assert.equal(instance.count, "0");
});

test("without the rail's seed the server's rendering stays", () => {
  const { instance, el } = setup({ tasks: [], seeded: false, painted: 2 });
  assert.equal(instance.hasChildren, true, "whatever the server painted shows");
  assert.ok(el.querySelector("[data-ssr]") !== null);
});

test("the strip's button and panel read the children: count, phrase, header line and scroll label", () => {
  const { instance, store } = setup({ tasks: [featureTask, makeTask({ id: "c1", status: "review", parentId: "f1" }), makeTask({ id: "c2", status: "todo", parentId: "f1" })] });
  assert.deepEqual([instance.count, instance.phrase, instance.showing, instance.scrollLabel], ["2", "1 in review", "Showing 2 tasks", "Child tasks, 2"]);
  store.tasks = store.tasks.filter((t) => t.id !== "c2");
  assert.deepEqual([instance.showing, instance.scrollLabel], ["Showing 1 task", "Child tasks, 1"]);
});
