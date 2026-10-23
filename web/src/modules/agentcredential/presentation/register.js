// The agent credential module's Alpine components: the Agent credential
// section of the Agent isolation page.

import { agentCredential } from "./components/agentCredential.js";

/**
 * @param {import("alpinejs").Alpine} Alpine
 * @param {{ gateway: import("../domain/ports.js").AgentCredentialGateway }} deps
 */
export function registerAgentCredential(Alpine, deps) {
  Alpine.data("agentCredential", agentCredential(deps));
}
