// The projects module's Alpine components.

import { newProject } from "./components/newProject.js";
import { envFilesPage } from "./pages/envFilesPage.js";
import { projectsPage } from "./pages/projectsPage.js";
import { repositoriesPage } from "./pages/repositoriesPage.js";
import { settingsPage } from "./pages/settingsPage.js";

/**
 * @param {import("alpinejs").Alpine} Alpine
 * @param {{
 *   gateway: import("../domain/ports.js").ProjectGateway,
 *   navigate: (url: string) => void,
 *   clock: import("../../../shared/infrastructure/clock.js").Clock,
 * }} deps
 */
export function registerProjects(Alpine, deps) {
  Alpine.data("projectsNewProject", newProject(deps));
  Alpine.data("projectsRepositoriesPage", repositoriesPage(deps));
  Alpine.data("projectsEnvFilesPage", envFilesPage(deps));
  Alpine.data("projectsListPage", projectsPage(deps));
  Alpine.data("projectsSettingsPage", settingsPage(deps));
}
