// The usage module's Alpine components.

import { usagePage } from "./components/usagePage.js";

/**
 * @param {import("alpinejs").Alpine} Alpine
 * @param {{ gateway: import("../domain/ports.js").UsageGateway }} deps
 */
export function registerUsage(Alpine, deps) {
  Alpine.data("usagePage", usagePage(deps));
}
