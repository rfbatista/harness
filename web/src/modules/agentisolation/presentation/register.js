// The agent isolation module's Alpine components: the summary chips at the
// top of the Agent isolation page.

import { isolationSummary } from "./components/isolationSummary.js";

/** @param {import("alpinejs").Alpine} Alpine */
export function registerAgentIsolation(Alpine) {
  Alpine.data("isolationSummary", isolationSummary());
}
