// The projects module's Alpine components.

import { newProject } from "./components/newProject.js";
import { envFilesPage } from "./pages/envFilesPage.js";
import { repositoriesPage } from "./pages/repositoriesPage.js";

/**
 * @param {import("alpinejs").Alpine} Alpine
 * @param {{
 *   gateway: import("../domain/ports.js").ProjectGateway,
 *   navigate: (url: string) => void,
 * }} deps
 */
export function registerProjects(Alpine, deps) {
  Alpine.data("projectsNewProject", newProject(deps));
  Alpine.data("projectsRepositoriesPage", repositoriesPage(deps));
  Alpine.data("projectsEnvFilesPage", envFilesPage(deps));
}
