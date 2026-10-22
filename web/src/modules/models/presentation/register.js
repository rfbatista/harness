// The models module's Alpine components: each tier's model on the Models
// page.

import { modelTier } from "./components/modelTier.js";

/**
 * @param {import("alpinejs").Alpine} Alpine
 * @param {{ gateway: import("../domain/ports.js").ModelGateway }} deps
 */
export function registerModels(Alpine, deps) {
  Alpine.data("modelTier", modelTier(deps));
}
