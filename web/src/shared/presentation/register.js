// Shell-wide components, available on every page.

import { listbox } from "./components/listbox.js";
import { projectPicker } from "./components/projectPicker.js";
import { streamStatus } from "./components/streamStatus.js";
import { themeToggle } from "./components/themeToggle.js";

/**
 * @param {import("alpinejs").Alpine} Alpine
 * @param {{ prefs: import("../infrastructure/storage.js").Preferences }} deps
 */
export function registerShared(Alpine, { prefs }) {
  Alpine.data("listbox", listbox());
  Alpine.data("projectPicker", projectPicker());
  Alpine.data("streamStatus", streamStatus());
  Alpine.data("themeToggle", themeToggle(prefs));
}
