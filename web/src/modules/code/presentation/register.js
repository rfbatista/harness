// The code module's Alpine components: the session's Code tab, the
// repository code page, and the diffs on the history pages.

import { codeDiff } from "./components/codeDiff.js";
import { codePage } from "./components/codePage.js";
import { codePanel } from "./components/codePanel.js";

/**
 * @param {import("alpinejs").Alpine} Alpine
 * @param {import("./components/codeBrowser.js").BrowserDeps & { navigate: (url: string) => void }} deps
 */
export function registerCode(Alpine, deps) {
  Alpine.data("codePanel", codePanel(deps));
  Alpine.data("codePage", codePage(deps));
  Alpine.data("codeDiff", codeDiff(deps));
}
