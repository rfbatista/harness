// The settings module's Alpine components: the flags on the Server page.

import { settingFlag } from "./components/settingFlag.js";

/**
 * @param {import("alpinejs").Alpine} Alpine
 * @param {{ gateway: import("../domain/ports.js").SettingsGateway }} deps
 */
export function registerSettings(Alpine, deps) {
  Alpine.data("settingFlag", settingFlag(deps));
}
