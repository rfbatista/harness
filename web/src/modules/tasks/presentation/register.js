// The tasks module's Alpine components.

import { newTask } from "./components/newTask.js";
import { rail, railLink } from "./components/rail.js";
import { taskEditor } from "./components/taskEditor.js";

/**
 * @param {import("alpinejs").Alpine} Alpine
 * @param {{
 *   gateway: import("../domain/ports.js").TaskGateway,
 *   rail: import("../domain/ports.js").RailGateway,
 *   navigate: (url: string) => void,
 *   reload: () => void,
 * }} deps
 */
export function registerTasks(Alpine, deps) {
  Alpine.data("tasksNewTask", newTask(deps));
  Alpine.data("tasksTaskEditor", taskEditor(deps));

  // The rail's per-task activity, shared by the rail and its links.
  Alpine.store("tasksRail", { byTask: {} });
  const store = Alpine.store("tasksRail");
  Alpine.data("tasksRail", rail({ gateway: deps.rail, store }));
  Alpine.data("tasksRailLink", railLink({ store }));
}
