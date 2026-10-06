import { Codes } from "../../../../shared/domain/errors.js";
import { mount, seededElement } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { memoryTasks } from "../../infrastructure/memory-gateway.js";
import { makeTask, taskDTO } from "../../testing/fixtures.js";
import { taskEditor } from "./taskEditor.js";

file("tasks/presentation/taskEditor");

function setup({ task = makeTask(), sessions = [] } = {}) {
  const memory = memoryTasks({ projects: ["p1"], tasks: [task], sessions });
  const visited = [];
  let reloads = 0;
  const el = seededElement(taskDTO(task));
  const mounted = mount(taskEditor({ gateway: memory.gateway, navigate: (u) => visited.push(u), reload: () => reloads++ }), { el });
  mounted.instance.init();
  return { ...mounted, memory, visited, reloads: () => reloads };
}

test("a status change is saved status-only and the page reloads so the rail regroups", async () => {
  const { instance, memory, reloads } = setup();
  assert.equal(instance.status, "in_progress");
  instance.status = "review";
  await instance.changeStatus();
  assert.equal(memory.tasks()[0].status, "review");
  assert.deepEqual(memory.calls.at(-1), { updateTask: { id: "t1", status: "review" } });
  assert.equal(reloads(), 1);
});

test("editing the text does not resend the status", async () => {
  const { instance, memory } = setup();
  instance.startEditTask();
  instance.draftTitle = "Add the SSE feed";
  await instance.save();
  assert.deepEqual(memory.calls.at(-1), { updateTask: { id: "t1", title: "Add the SSE feed", description: "Stream session changes to clients." } });
});

test("a refused status change goes back to the saved one", async () => {
  const { instance, memory, reloads } = setup();
  await memory.gateway.deleteTask("t1"); // gone meanwhile
  instance.status = "done";
  await instance.changeStatus();
  assert.equal(instance.status, "in_progress");
  assert.equal(instance.taskError.code, Codes.TICKET_NOT_FOUND);
  assert.equal(reloads(), 0);
});

test("edits the title and description in place", async () => {
  const { instance, memory } = setup();
  instance.startEditTask();
  assert.equal(instance.draftTitle, "Add SSE feed");
  instance.draftTitle = "  ";
  assert.equal(instance.cannotSaveTask, true);
  instance.draftTitle = "Add the SSE feed";
  instance.draftDescription = "Follow every session change.";
  await instance.save();
  assert.equal(instance.editingTask, false);
  assert.deepEqual([instance.title, memory.tasks()[0].description], ["Add the SSE feed", "Follow every session change."]);
  assert.equal(memory.tasks()[0].status, "in_progress", "editingTask keeps the status");
});

test("a task with sessions is not deleted", async () => {
  const { instance, memory } = setup({ sessions: [{ ticketId: "t1", status: "idle" }] });
  await instance.askDeleteTask();
  assert.equal(instance.confirmingTaskDelete, false);
  assert.ok(instance.taskDeleteProblem.includes("running session"));
  assert.equal(memory.tasks().length, 1);
});

test("a task without sessions is deleted after confirming, then the project opens", async () => {
  const { instance, memory, visited } = setup();
  await instance.askDeleteTask();
  assert.equal(instance.confirmingTaskDelete, true);
  instance.cancelDeleteTask();
  assert.equal(instance.confirmingTaskDelete, false);
  await instance.askDeleteTask();
  await instance.removeTask();
  assert.deepEqual(memory.tasks(), []);
  assert.deepEqual(visited, ["/projects/p1"]);
});
