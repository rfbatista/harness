// The sessions module's Alpine components. Names are global: module
// components carry the module prefix.

import { replyBox } from "./components/replyBox.js";
import { sessionsPage } from "./pages/sessionsPage.js";

/**
 * @param {import("alpinejs").Alpine} Alpine
 * @param {{
 *   gateway: import("../domain/ports.js").SessionGateway,
 *   clock: import("../../../shared/infrastructure/clock.js").Clock,
 * }} deps
 */
export function registerSessions(Alpine, deps) {
  Alpine.data("sessionsPage", sessionsPage(deps));
  Alpine.data("sessionsReplyBox", replyBox(deps));
}
