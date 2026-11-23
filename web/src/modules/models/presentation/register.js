// The models module's Alpine components: each tier's model on the Models
// page and the Mode agents section.

import { modeAgents } from "./components/modeAgents.js";
import { modelTier } from "./components/modelTier.js";

/**
 * @param {import("alpinejs").Alpine} Alpine
 * @param {{ gateway: import("../domain/ports.js").ModelGateway & import("../domain/ports.js").ModeAgentsGateway }} deps
 */
export function registerModels(Alpine, deps) {
  Alpine.data("modelTier", modelTier(deps));
  Alpine.data("modeAgents", modeAgents(deps));
}
