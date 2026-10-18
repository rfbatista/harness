// The tokensavers module's Alpine components: the Token savers control of
// the settings pages and a task's detail, and the Token savers page with
// its Repository indexes and Caveman intensity sections.

import { activations } from "./components/activations.js";
import { cavemanIntensity } from "./components/cavemanIntensity.js";
import { repositoryIndexes } from "./components/repositoryIndexes.js";
import { tokenSaversPage } from "./components/tokenSaversPage.js";

/**
 * @param {import("alpinejs").Alpine} Alpine
 * @param {{
 *   gateway: import("../domain/ports.js").TokenSaverGateway,
 *   clock: import("../../../shared/infrastructure/clock.js").Clock,
 * }} deps
 */
export function registerTokenSavers(Alpine, deps) {
  Alpine.data("tokenSaversActivations", activations(deps));
  Alpine.data("tokenSaversPage", tokenSaversPage(deps));
  Alpine.data("repositoryIndexes", repositoryIndexes(deps));
  Alpine.data("cavemanIntensity", cavemanIntensity(deps));
}
