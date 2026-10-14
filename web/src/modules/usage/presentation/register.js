// The usage module's Alpine components. The task page's totals watch and
// its chips share one reactive store.

import { usagePage } from "./components/usagePage.js";
import { emptyTotalsStore, usageTotalsChip, usageTotalsWatch } from "./components/usageTotals.js";

/**
 * @param {import("alpinejs").Alpine} Alpine
 * @param {{ gateway: import("../domain/ports.js").UsageGateway }} deps
 */
export function registerUsage(Alpine, { gateway }) {
  const store = Alpine.reactive(emptyTotalsStore());
  Alpine.data("usagePage", usagePage({ gateway }));
  Alpine.data("usageTotalsWatch", usageTotalsWatch({ gateway, store }));
  Alpine.data("usageTotalsChip", usageTotalsChip({ store }));
}
