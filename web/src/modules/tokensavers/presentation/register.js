// The tokensavers module's Alpine components.

import { activations } from "./components/activations.js";

/**
 * @param {import("alpinejs").Alpine} Alpine
 * @param {{ gateway: import("../domain/ports.js").TokenSaverGateway }} deps
 */
export function registerTokenSavers(Alpine, deps) {
  Alpine.data("tokenSaversActivations", activations(deps));
}
