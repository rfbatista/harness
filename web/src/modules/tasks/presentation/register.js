// The tasks module's Alpine components.

import { archived } from "./components/archived.js";
import { board } from "./components/board.js";
import { documentScope } from "./components/documentScope.js";
import { documentWatch } from "./components/documentWatch.js";
import { feature } from "./components/feature.js";
import { newTask } from "./components/newTask.js";
import { railFeed } from "./components/railFeed.js";
import { resourceWatch } from "./components/resourceWatch.js";
import { taskEditor } from "./components/taskEditor.js";
import { taskResources } from "./components/taskResources.js";

/**
 * @param {import("alpinejs").Alpine} Alpine
 * @param {{
 *   gateway: import("../domain/ports.js").TaskGateway,
 *   rail: import("../domain/ports.js").RailGateway,
 *   prefs: import("../../../shared/infrastructure/storage.js").Preferences,  the browser's own choices (localStorage)
 *   navigate: (url: string) => void,
 *   reload: () => void,
 *   resources: import("../domain/ports.js").ResourceGateway,
 *   tokenSavers: import("../../tokensavers/domain/ports.js").TokenSaverGateway,  the task editor writes the task's savers
 *   recording: ReturnType<typeof import("../infrastructure/recording-gateway.js").recordingGateway>,  the task editor writes the task's trajectory recording
 *   clock: import("../../../shared/infrastructure/clock.js").Clock,
 * }} deps
 */
export function registerTasks(Alpine, deps) {
  Alpine.data("tasksNewTask", newTask(deps));
  Alpine.data("tasksDocumentWatch", documentWatch(deps));
  Alpine.data("tasksDocumentScope", documentScope(deps));
  Alpine.data("tasksResources", taskResources(deps));
  Alpine.data("tasksResourceWatch", resourceWatch(deps));

  // The project's live model, kept by the headless feed owner and read by the board, the archived and feature pages and the task editor.
  Alpine.store("tasksRail", { byTask: {}, tasks: [], seeded: false, statusChanges: {}, archivedCount: 0 });
  const store = Alpine.store("tasksRail");
  Alpine.data("tasksTaskEditor", taskEditor({ ...deps, store }));
  Alpine.data("tasksRailFeed", railFeed({ gateway: deps.rail, store }));
  Alpine.data("tasksBoard", board({ gateway: deps.gateway, store, prefs: deps.prefs, clock: deps.clock }));
  Alpine.data("tasksArchived", archived({ gateway: deps.gateway }));
  Alpine.data("tasksFeature", feature({ store }));
}
