// The Token savers page's Repository indexes section (Alpine component).

import { repositoryIndexes } from "./components/repositoryIndexes.js";

/**
 * @param {import("alpinejs").Alpine} Alpine
 * @param {{ gateway: import("../domain/repository-index-ports.js").RepositoryIndexGateway, clock: import("../../../shared/infrastructure/clock.js").Clock }} deps
 */
export function registerRepositoryIndexes(Alpine, deps) {
  Alpine.data("repositoryIndexes", repositoryIndexes(deps));
}
