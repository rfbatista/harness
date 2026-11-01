// The design system module's Alpine components.

import { designSystemPage } from "./pages/designSystemPage.js";

/**
 * @param {import("alpinejs").Alpine} Alpine
 * @param {{ gateway: ReturnType<typeof import("../infrastructure/designsystem-gateway.js").designSystemGateway>, clock: import("../../../shared/infrastructure/clock.js").Clock, clipboard?: { copyText: (text: string) => Promise<boolean> }, history?: History, location?: Location }} deps
 */
export function registerDesignSystem(Alpine, deps) {
  Alpine.data("designSystemPage", designSystemPage(deps));
}
