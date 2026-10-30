// The sync module's Alpine components. The page's watch, the chips, the
// panels and the fetch buttons share one reactive store and one set of
// actions, so an in-flight Fetch or Push is seen by all of them.

import { emptyStore, syncActions } from "./components/syncStore.js";
import { syncChip } from "./components/syncChip.js";
import { syncFetch } from "./components/syncFetch.js";
import { syncPanel } from "./components/syncPanel.js";
import { syncPopover } from "./components/syncPopover.js";
import { syncWatch } from "./components/syncWatch.js";

/**
 * @param {import("alpinejs").Alpine} Alpine
 * @param {{ gateway: import("../domain/ports.js").SyncGateway, now?: () => Date }} deps
 */
export function registerSync(Alpine, { gateway, now = () => new Date() }) {
  const store = Alpine.reactive(emptyStore());
  const actions = syncActions({ gateway, store });
  Alpine.data("syncWatch", syncWatch({ gateway, actions }));
  Alpine.data("syncChip", syncChip({ store, actions, now }));
  Alpine.data("syncPanel", syncPanel({ store, actions, now }));
  Alpine.data("syncFetch", syncFetch({ store, actions, now }));
  Alpine.data("syncPopover", syncPopover({ store, actions, now }));
}
