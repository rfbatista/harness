// The managed claude module's Alpine components: the Managed claude section
// of the Agent isolation page.

import { managedClaude } from "./components/managedClaude.js";

/**
 * @param {import("alpinejs").Alpine} Alpine
 * @param {{ gateway: import("../domain/ports.js").ClaudeGateway, pollMs?: number }} deps
 */
export function registerClaude(Alpine, deps) {
  Alpine.data("managedClaude", managedClaude(deps));
}
