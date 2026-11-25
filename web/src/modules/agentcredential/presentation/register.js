// The agent credential module's Alpine components: the Agent credential
// section of the Agent isolation page, and a project's own credential on its
// settings page.

import { agentCredential } from "./components/agentCredential.js";
import { projectAgentCredential } from "./components/projectAgentCredential.js";

/**
 * @param {import("alpinejs").Alpine} Alpine
 * @param {{ gateway: import("../domain/ports.js").AgentCredentialGateway }} deps
 */
export function registerAgentCredential(Alpine, deps) {
  Alpine.data("agentCredential", agentCredential(deps));
  Alpine.data("projectAgentCredential", projectAgentCredential(deps));
}
