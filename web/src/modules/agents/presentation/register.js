// The agents module's Alpine components: the Agents page.

import { agentsPage } from "./components/agentsPage.js";

/**
 * @param {import("alpinejs").Alpine} Alpine
 * @param {{
 *   gateway: import("../domain/ports.js").CatalogGateway,
 *   history: Pick<History, "pushState">,
 *   location: Pick<Location, "pathname">,
 * }} deps
 */
export function registerAgents(Alpine, deps) {
  Alpine.data("agentsPage", agentsPage(deps));
}
