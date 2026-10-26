// The runs module's Alpine components.

import { appPanel } from "./components/appPanel.js";
import { runCommandsSettings } from "./components/runCommandsSettings.js";

/**
 * @param {import("alpinejs").Alpine} Alpine
 * @param {{ gateway: import("../domain/ports.js").RunGateway, prefs: import("../../../shared/infrastructure/storage.js").Preferences }} deps
 */
export function registerRuns(Alpine, deps) {
  Alpine.data("runsAppPanel", appPanel(deps));
  Alpine.data("runsCommandsSettings", runCommandsSettings(deps));
}
