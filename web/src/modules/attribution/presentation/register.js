// The attribution module's Alpine components: the Agent attribution section
// of the Agent isolation page.

import { agentAttribution } from "./components/agentAttribution.js";

/**
 * @param {import("alpinejs").Alpine} Alpine
 * @param {{ gateway: import("../domain/ports.js").AttributionGateway }} deps
 */
export function registerAttribution(Alpine, deps) {
  Alpine.data("agentAttribution", agentAttribution(deps));
}
