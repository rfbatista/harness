// The sessions module's Alpine components. Names are global: module
// components carry the module prefix.

import { designLibrary } from "./components/designLibrary.js";
import { designPanel } from "./components/designPanel.js";
import { newSession } from "./components/newSession.js";
import { terminal } from "./components/terminal.js";
import { sessionsPage } from "./pages/sessionsPage.js";

/**
 * @param {import("alpinejs").Alpine} Alpine
 * @param {{
 *   gateway: import("../domain/ports.js").SessionGateway,
 *   terminals: import("../domain/ports.js").TerminalGateway,
 *   artifacts: import("../domain/ports.js").ArtifactGateway,
 *   runTerminals?: import("../domain/ports.js").TerminalGateway,  application runs' terminals
 *   createScreen: import("./components/terminal.js").CreateScreen,
 *   clock: import("../../../shared/infrastructure/clock.js").Clock,
 * }} deps
 */
export function registerSessions(Alpine, deps) {
  Alpine.data("sessionsPage", sessionsPage(deps));
  Alpine.data("sessionsNewSession", newSession(deps));
  Alpine.data("sessionsTerminal", terminal(deps));
  Alpine.data("sessionsDesignPanel", designPanel(deps));
  Alpine.data("sessionsDesignLibrary", designLibrary(deps));
  // The same terminal, on an application run started from a session.
  if (deps.runTerminals) Alpine.data("sessionsRunTerminal", terminal({ ...deps, terminals: deps.runTerminals }));
}
