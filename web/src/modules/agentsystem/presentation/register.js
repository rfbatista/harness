// The agent system module's Alpine components: the Guard line the pages that
// list repositories carry (the Agent system page itself is a React island).

import { guardLineComponent } from "./components/guardLine.js";

/**
 * @param {import("alpinejs").Alpine} Alpine
 * @param {{ gateway: ReturnType<typeof import("../infrastructure/guard-gateway.js").guardGateway> }} deps
 */
export function registerAgentSystem(Alpine, { gateway }) {
  Alpine.data("agentSystemGuardLine", guardLineComponent({ gateway }));
}
