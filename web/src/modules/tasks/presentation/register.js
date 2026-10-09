// The tasks module's Alpine components.

import { board } from "./components/board.js";
import { documentScope } from "./components/documentScope.js";
import { documentWatch } from "./components/documentWatch.js";
import { newTask } from "./components/newTask.js";
import { rail } from "./components/rail.js";
import { resourceWatch } from "./components/resourceWatch.js";
import { taskEditor } from "./components/taskEditor.js";
import { taskResources } from "./components/taskResources.js";

/**
 * @param {import("alpinejs").Alpine} Alpine
 * @param {{
 *   gateway: import("../domain/ports.js").TaskGateway,
 *   rail: import("../domain/ports.js").RailGateway,
 *   navigate: (url: string) => void,
 *   reload: () => void,
 *   resources: import("../domain/ports.js").ResourceGateway,
 *   clock: import("../../../shared/infrastructure/clock.js").Clock,
 * }} deps
 */
export function registerTasks(Alpine, deps) {
  Alpine.data("tasksNewTask", newTask(deps));
  Alpine.data("tasksDocumentWatch", documentWatch(deps));
  Alpine.data("tasksDocumentScope", documentScope(deps));
  Alpine.data("tasksResources", taskResources(deps));
  Alpine.data("tasksResourceWatch", resourceWatch(deps));

  // The project's live model, shared by the rail, its links and the board.
  Alpine.store("tasksRail", { byTask: {}, tasks: [], seeded: false, statusChanges: {} });
  const store = Alpine.store("tasksRail");
  Alpine.data("tasksTaskEditor", taskEditor({ ...deps, store }));
  Alpine.data("tasksRail", rail({ gateway: deps.rail, store }));
  Alpine.data("tasksBoard", board({ gateway: deps.gateway, store }));
}
