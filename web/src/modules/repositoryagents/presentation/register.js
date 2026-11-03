// The repository agents module's Alpine components.

import { repositoryAgentsTab } from "./components/repositoryAgentsTab.js";

/**
 * @param {import("alpinejs").Alpine} Alpine
 * @param {{ gateway: import("../domain/ports.js").RepositoryAgentsGateway }} deps
 */
export function registerRepositoryAgents(Alpine, deps) {
  Alpine.data("repositoryAgentsTab", repositoryAgentsTab(deps));
}
