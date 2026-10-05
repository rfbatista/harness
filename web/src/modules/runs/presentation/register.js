// The runs module's Alpine components.

import { appPanel } from "./components/appPanel.js";

/**
 * @param {import("alpinejs").Alpine} Alpine
 * @param {{ gateway: import("../domain/ports.js").RunGateway }} deps
 */
export function registerRuns(Alpine, deps) {
  Alpine.data("runsAppPanel", appPanel(deps));
}
