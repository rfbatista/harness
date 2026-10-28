import { mount, seededElement } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { makeTask } from "../../testing/fixtures.js";
import { feature } from "./feature.js";

file("tasks/presentation/feature");

function setup({ tasks, seeded = true, documents = {}, painted = 0, hash = "" } = {}) {
  const store = { byTask: {}, tasks, seeded };
  const el = seededElement({ documents }, "feature-seed");
  el.dataset.taskId = "f1";
  el.dataset.projectId = "p1";
  el.dataset.children = String(painted);
  const ssr = el.ownerDocument.createElement("div");
  ssr.dataset.ssr = "";
  el.append(ssr);
  const mounted = mount(feature({ store, hash: () => hash }), { el });
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

test("the button and popover read the children: count, name, header line and scroll label", () => {
  const { instance, store } = setup({ tasks: [featureTask, makeTask({ id: "c1", status: "review", parentId: "f1" }), makeTask({ id: "c2", status: "todo", parentId: "f1" })] });
  assert.deepEqual([instance.count, instance.buttonName, instance.scrollLabel], ["2", "Feature, 2 children, 1 in review", "Child tasks, 2"]);
  store.tasks = store.tasks.filter((t) => t.id !== "c2");
  assert.deepEqual([instance.buttonName, instance.scrollLabel], ["Feature, 1 child, 1 in review", "Child tasks, 1"]);
});

test("the rows are listed in review, in progress, todo, backlog, done order, whatever order the tasks come in", () => {
  const status = ["done", "backlog", "todo", "in_progress", "review", "todo"];
  const { instance } = setup({ tasks: [featureTask, ...status.map((st, i) => makeTask({ id: `c${i}`, status: st, parentId: "f1" }))] });
  assert.deepEqual(instance.children.map((c) => c.status), ["review", "in_progress", "todo", "todo", "backlog", "done"]);
});

test("the popover's foot says how many children are in each status, in that order, and follows the feed", () => {
  const status = ["review", "review", "in_progress", "todo", "todo", "todo", "done"];
  const { instance, store } = setup({ tasks: [featureTask, ...status.map((st, i) => makeTask({ id: `c${i}`, status: st, parentId: "f1" }))] });
  assert.equal(instance.footLine, "2 in review · 1 in progress · 3 todo · 1 done");
  store.tasks = store.tasks.map((t) => (t.id === "c3" ? { ...t, status: "review" } : t));
  assert.equal(instance.footLine, "3 in review · 1 in progress · 2 todo · 1 done");
});

test("the button opens the popover under itself; Esc, Close or a press outside leave it closed and nothing is remembered", () => {
  const { instance, dispatched } = setup({ tasks: [featureTask, makeTask({ id: "c1", parentId: "f1" })] });
  const anchor = document.createElement("button");
  anchor.id = "task-feature-button";
  document.body.append(anchor);
  assert.equal(instance.popoverOpen, false);
  instance.openPopover();
  assert.equal(instance.popoverOpen, true);
  assert.deepEqual(dispatched.at(-1), { name: "dialog-open", detail: { id: "task-feature", anchor } });
  instance.popoverClosed({ detail: { id: "task-reviews" } });
  assert.equal(instance.popoverOpen, true, "another dialog closing is not this one");
  instance.popoverClosed({ detail: { id: "task-feature" } });
  assert.equal(instance.popoverOpen, false);
  anchor.remove();
});

test("a link to #feature opens the popover once there are children; other links do not", () => {
  const withChildren = [featureTask, makeTask({ id: "c1", parentId: "f1" })];
  assert.equal(setup({ tasks: withChildren, hash: "#feature" }).instance.popoverOpen, true);
  assert.equal(setup({ tasks: withChildren, hash: "#reviews" }).instance.popoverOpen, false);
  assert.equal(setup({ tasks: withChildren }).instance.popoverOpen, false);
  assert.equal(setup({ tasks: [featureTask], hash: "#feature" }).instance.popoverOpen, false, "nothing to show");
});
