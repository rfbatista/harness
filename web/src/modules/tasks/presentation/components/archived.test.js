import { Codes, StructuredError } from "../../../../shared/domain/errors.js";
import { mount, seededElement } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { memoryTasks } from "../../infrastructure/memory-gateway.js";
import { makeTask, taskDTO } from "../../testing/fixtures.js";
import { archived } from "./archived.js";

file("tasks/presentation/archived");

const archivedTask = (id, title, archivedAt) => makeTask({ id, title, status: "done", archivedAt });

function setup(tasks = [archivedTask("a", "Ship it", 3), archivedTask("b", "Docs", 2)]) {
  const memory = memoryTasks({ projects: ["p1"], tasks });
  const el = seededElement({ project_id: "p1", tickets: tasks.map(taskDTO) }, "archived-seed");
  const mounted = mount(archived({ gateway: memory.gateway }), { el });
  mounted.instance.init();
  return { ...mounted, memory };
}

/** A click on a row's button. */
const on = (taskId) => {
  const row = document.createElement("li");
  row.dataset.taskId = taskId;
  const button = document.createElement("button");
  row.append(button);
  return { target: button };
};

const changed = (change, previous = null) => ({ detail: { change, previous } });

test("the list starts from the seed, newest archived first, with its summary", () => {
  const { instance } = setup();
  assert.deepEqual(instance.rows.map((r) => r.id), ["a", "b"]);
  assert.equal(instance.summary, "2 archived tasks. Restore puts a task back in Done. Delete removes it and everything it holds.");
  assert.equal(instance.isEmpty, false);
});

test("Restore puts the task back in Done and its row becomes a receipt that stays", async () => {
  const { instance, memory } = setup();
  const p = instance.restore(on("a"));
  assert.equal(instance.rows[0].busyText, "Restoring…");
  await p;
  assert.equal(memory.tasks().find((t) => t.id === "a").archivedAt, 0);
  assert.deepEqual([instance.rows[0].receipt, instance.summary.startsWith("1 archived task."), instance.announcement], ["Ship it restored to Done.", true, "Restored Ship it to Done."]);
  instance.taskChanged(changed({ kind: "task-upsert", task: makeTask({ id: "a", title: "Ship it", status: "done" }) }));
  assert.equal(instance.rows[0].receipt, "Ship it restored to Done.", "its own echo changes nothing");
});

test("Delete asks first, in the row; Cancel keeps it; Delete forever removes it", async () => {
  const { instance, memory } = setup();
  instance.askDelete(on("b"));
  assert.deepEqual([instance.rows[1].confirming, instance.rows[0].confirming], [true, false]);
  instance.cancelDelete();
  assert.equal(instance.rows[1].confirming, false);
  assert.equal(memory.tasks().length, 2);
  instance.askDelete(on("b"));
  await instance.confirmDelete(on("b"));
  assert.deepEqual([memory.tasks().map((t) => t.id), instance.rows[1].receipt], [["a"], "Deleted Docs."]);
});

test("a failed restore keeps the row with its error under it", async () => {
  const { instance, memory } = setup();
  memory.gateway.restoreTask = async () => {
    throw new StructuredError(Codes.NETWORK, "The harness server is not reachable.");
  };
  await instance.restore(on("a"));
  assert.deepEqual([instance.rows[0].receipt, instance.rows[0].busy, instance.rows[0].error.code], ["", false, Codes.NETWORK]);
});

test("live: a task archived elsewhere appears on top; one restored or deleted elsewhere leaves with its receipt", () => {
  const { instance } = setup();
  instance.taskChanged(changed({ kind: "task-upsert", task: archivedTask("c", "Feed", 9) }, makeTask({ id: "c", status: "done" })));
  assert.deepEqual(instance.rows.map((r) => r.id), ["c", "a", "b"]);
  instance.taskChanged(changed({ kind: "task-upsert", task: makeTask({ id: "a", title: "Ship it", status: "done" }) }));
  instance.taskChanged(changed({ kind: "task-deleted", id: "b" }));
  assert.deepEqual(instance.rows.map((r) => r.receipt), ["", "Ship it restored to Done.", "Deleted Docs."]);
  assert.ok(instance.summary.startsWith("1 archived task."));
});

test("nothing archived shows the empty state", () => {
  const { instance } = setup([]);
  assert.deepEqual([instance.isEmpty, instance.summary], [true, ""]);
});
