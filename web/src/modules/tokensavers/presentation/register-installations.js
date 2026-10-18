// The Token savers page's Alpine component (installations).

import { tokenSaversPage } from "./components/tokenSaversPage.js";

/**
 * @param {import("alpinejs").Alpine} Alpine
 * @param {{ gateway: import("../domain/installation-ports.js").TokenSaverInstallGateway }} deps
 */
export function registerTokenSaverInstalls(Alpine, deps) {
  Alpine.data("tokenSaversPage", tokenSaversPage(deps));
}
