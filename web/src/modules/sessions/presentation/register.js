// The sessions module's Alpine components. Names are global: module
// components carry the module prefix.

import { newSession } from "./components/newSession.js";
import { terminal } from "./components/terminal.js";
import { sessionsPage } from "./pages/sessionsPage.js";

/**
 * @param {import("alpinejs").Alpine} Alpine
 * @param {{
 *   gateway: import("../domain/ports.js").SessionGateway,
 *   terminals: import("../domain/ports.js").TerminalGateway,
 *   createScreen: import("./components/terminal.js").CreateScreen,
 *   clock: import("../../../shared/infrastructure/clock.js").Clock,
 * }} deps
 */
export function registerSessions(Alpine, deps) {
  Alpine.data("sessionsPage", sessionsPage(deps));
  Alpine.data("sessionsNewSession", newSession(deps));
  Alpine.data("sessionsTerminal", terminal(deps));
}
