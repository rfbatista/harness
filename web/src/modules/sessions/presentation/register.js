// The sessions module's Alpine components. Names are global: module
// components carry the module prefix.

import { conversation } from "./components/conversation.js";
import { designLibrary } from "./components/designLibrary.js";
import { designWatch } from "./components/designWatch.js";
import { taskDesign } from "./components/taskDesign.js";
import { designPanel } from "./components/designPanel.js";
import { newSession } from "./components/newSession.js";
import { terminal } from "./components/terminal.js";
import { statusCheck } from "./components/statusCheck.js";
import { sessionsPage } from "./pages/sessionsPage.js";
import { screenPrefs } from "./screenPrefs.js";

/**
 * @param {import("alpinejs").Alpine} Alpine
 * @param {{
 *   gateway: import("../domain/ports.js").SessionGateway,
 *   terminals: import("../domain/ports.js").TerminalGateway,
 *   artifacts: import("../domain/ports.js").ArtifactGateway,
 *   runTerminals?: import("../domain/ports.js").TerminalGateway,  application runs' terminals
 *   createScreen: import("./components/terminal.js").CreateScreen,
 *   clock: import("../../../shared/infrastructure/clock.js").Clock,
 *   channel?: import("../domain/ports.js").ChannelGateway,  the architect channel
 *   prefs?: import("../../../shared/infrastructure/storage.js").Preferences,  this browser's text size and focus mode
 * }} deps
 */
export function registerSessions(Alpine, deps) {
  // The task's messages between its architect and the delegates, shared by
  // the page and its Conversation tab.
  Alpine.store("sessionsChannel", { messages: [], loaded: false });
  const channelStore = Alpine.store("sessionsChannel");
  const screen = deps.prefs ? screenPrefs(deps.prefs) : undefined;
  const shell = { setFocus: (on) => globalThis.document?.querySelector(".app-shell")?.toggleAttribute("data-focus", on) };
  Alpine.data("sessionsPage", sessionsPage({ ...deps, channelStore, prefs: screen, shell }));
  Alpine.data("sessionsConversation", conversation({ ...deps, channelStore }));
  Alpine.data("sessionsStatusCheck", statusCheck(deps));
  Alpine.data("sessionsNewSession", newSession(deps));
  const textSize = () => screen?.textSize();
  Alpine.data("sessionsTerminal", terminal({ ...deps, textSize }));
  Alpine.data("sessionsDesignPanel", designPanel(deps));
  Alpine.data("sessionsDesignLibrary", designLibrary(deps));
  Alpine.data("sessionsTaskDesign", taskDesign(deps));
  Alpine.data("sessionsDesignWatch", designWatch(deps));
  // The same terminal, on an application run started from a session.
  if (deps.runTerminals) Alpine.data("sessionsRunTerminal", terminal({ ...deps, terminals: deps.runTerminals, kind: "run", textSize }));
}
