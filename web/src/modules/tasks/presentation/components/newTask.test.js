import { Codes } from "../../../../shared/domain/errors.js";
import { mount } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { memoryTasks } from "../../infrastructure/memory-gateway.js";
import { newTask } from "./newTask.js";

file("tasks/presentation/newTask");

function setup(projectId = "p1") {
  const memory = memoryTasks({ projects: ["p1"] });
  const visited = [];
  const mounted = mount(() => newTask({ gateway: memory.gateway, navigate: (u) => visited.push(u) })(projectId));
  return { ...mounted, memory, visited };
}

test("creates the task and opens it", async () => {
  const { instance, memory, visited } = setup();
  assert.equal(instance.cannotSubmit, true, "a title is required");
  assert.equal(instance.status, "todo");
  instance.title = " Write docs ";
  instance.description = "The TUI's live section.";
  await instance.submit();
  const [task] = memory.tasks();
  assert.deepEqual([task.title, task.description, task.status], ["Write docs", "The TUI's live section.", "todo"]);
  assert.deepEqual(visited, [`/projects/p1/tasks/${task.id}`]);
});

test("the project comes from the form's data-project-id", () => {
  const memory = memoryTasks({ projects: ["p1"] });
  const form = document.createElement("form");
  form.dataset.projectId = "p1";
  const { instance } = mount(newTask({ gateway: memory.gateway, navigate: () => {} }), { el: form });
  instance.init();
  assert.equal(instance.projectId, "p1");
});

test("a refusal shows the coded error and keeps the draft", async () => {
  const { instance, visited } = setup("gone");
  instance.title = "Write docs";
  await instance.submit();
  assert.equal(instance.error.code, Codes.PROJECT_NOT_FOUND);
  assert.equal(instance.title, "Write docs");
  assert.deepEqual(visited, []);
});
