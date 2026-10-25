// The architecture module's Alpine components.

import { architecturePage } from "./pages/architecturePage.js";

/**
 * @param {import("alpinejs").Alpine} Alpine
 * @param {{ gateway: ReturnType<typeof import("../infrastructure/architecture-gateway.js").architectureGateway> }} deps
 */
export function registerArchitecture(Alpine, deps) {
  Alpine.data("architecturePage", architecturePage(deps));
}
