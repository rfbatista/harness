// The Agent board module's Alpine components.

import { agentBoardPage } from "./pages/agentBoardPage.js";

/**
 * @param {import("alpinejs").Alpine} Alpine
 * @param {{
 *   gateway: import("../domain/ports.js").AgentBoardGateway,
 *   navigate: (url: string) => void,
 *   clock: import("../../../shared/infrastructure/clock.js").Clock,
 * }} deps
 */
export function registerAgentBoard(Alpine, deps) {
  Alpine.data("agentBoardPage", agentBoardPage(deps));
}
