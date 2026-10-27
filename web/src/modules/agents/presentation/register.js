// The agents module's Alpine components: the Agents page and the MCP servers
// section of a project's settings.

import { agentsPage } from "./components/agentsPage.js";
import { projectMcpServers } from "./components/projectMcpServers.js";

/**
 * @param {import("alpinejs").Alpine} Alpine
 * @param {{
 *   gateway: import("../domain/ports.js").CatalogGateway,
 *   history: Pick<History, "pushState">,
 *   location: Pick<Location, "pathname">,
 *   clipboard?: Pick<Clipboard, "writeText">,
 * }} deps
 */
export function registerAgents(Alpine, deps) {
  Alpine.data("agentsPage", agentsPage(deps));
  Alpine.data("projectMcpServers", projectMcpServers({ gateway: deps.gateway }));
}
