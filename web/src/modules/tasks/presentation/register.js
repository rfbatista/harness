// The tasks module's Alpine components.

import { newTask } from "./components/newTask.js";
import { taskEditor } from "./components/taskEditor.js";

/**
 * @param {import("alpinejs").Alpine} Alpine
 * @param {{
 *   gateway: import("../domain/ports.js").TaskGateway,
 *   navigate: (url: string) => void,
 *   reload: () => void,
 * }} deps
 */
export function registerTasks(Alpine, deps) {
  Alpine.data("tasksNewTask", newTask(deps));
  Alpine.data("tasksTaskEditor", taskEditor(deps));
}
