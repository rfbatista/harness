// The design system module's Alpine components.

import { designSystemMenu } from "./components/designSystemMenu.js";
import { designSystemProposals } from "./components/designSystemProposals.js";
import { designSystemOptIn } from "./components/optIn.js";
import { repositoryDesignSystem } from "./components/repositoryDesignSystem.js";
import { designSystemPage } from "./pages/designSystemPage.js";
import { designSystemsPage } from "./pages/designSystemsPage.js";

/**
 * @param {import("alpinejs").Alpine} Alpine
 * @param {{ gateway: ReturnType<typeof import("../infrastructure/designsystem-gateway.js").designSystemGateway>, clock: import("../../../shared/infrastructure/clock.js").Clock, clipboard?: { copyText: (text: string) => Promise<boolean> }, history?: History, location?: Location, navigate?: (url: string) => void }} deps
 */
export function registerDesignSystem(Alpine, deps) {
  Alpine.data("designSystemPage", designSystemPage(deps));
  Alpine.data("designSystemsPage", designSystemsPage(deps));
  Alpine.data("designSystemMenu", designSystemMenu(deps));
  Alpine.data("designSystemProposals", designSystemProposals(deps));
  Alpine.data("designSystemOptIn", designSystemOptIn(deps));
  Alpine.data("repositoryDesignSystem", repositoryDesignSystem(deps));
}
