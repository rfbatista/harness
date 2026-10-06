// The tasks module's Alpine components.

import { documentWatch } from "./components/documentWatch.js";
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
  Alpine.data("tasksDocumentWatch", documentWatch(deps));

  // The project's live model, shared by the rail, its links and the board.
  Alpine.store("tasksRail", { byTask: {}, tasks: [], seeded: false });
  const store = Alpine.store("tasksRail");
  Alpine.data("tasksRail", rail({ gateway: deps.rail, store }));
  Alpine.data("tasksRailLink", railLink({ store }));
}
