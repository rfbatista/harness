// The sync module's Alpine components. The page's watch, the chips, the
// panels and the fetch buttons share one reactive store and one set of
// actions, so an in-flight Fetch or Push is seen by all of them.

import { emptyStore, syncActions } from "./components/syncStore.js";
import { skeletonRule } from "../../../shared/domain/skeletonRule.js";
import { syncBranchLink } from "./components/syncBranchLink.js";
import { syncChip, syncChipSkeleton } from "./components/syncChip.js";
import { syncFetch } from "./components/syncFetch.js";
import { syncPanel } from "./components/syncPanel.js";
import { syncPopover } from "./components/syncPopover.js";
import { syncStates, syncStatesLoading, syncWatch } from "./components/syncWatch.js";

/**
 * @param {import("alpinejs").Alpine} Alpine
 * @param {{ gateway: import("../domain/ports.js").SyncGateway, now?: () => Date, timers?: import("../../../shared/domain/skeletonRule.js").Timers }} deps
 */
export function registerSync(Alpine, { gateway, now = () => new Date(), timers }) {
  const store = Alpine.reactive(emptyStore());
  const actions = syncActions({ gateway, store });
  Alpine.data("syncWatch", syncWatch({ gateway, actions }));
  // The lazy git section's skeleton chips: one load per page, one rule.
  const loading = skeletonRule({
    timers,
    onChange: ({ visible, fade }) => {
      store.statesLoading = visible;
      if (fade) store.statesIn = "skeleton-in";
    },
  });
  Alpine.data("syncStates", syncStates({ gateway, actions, loading }));
  Alpine.data("syncStatesLoading", syncStatesLoading({ loading }));
  Alpine.data("syncChipSkeleton", syncChipSkeleton({ store }));
  Alpine.data("syncChip", syncChip({ store, actions, now }));
  Alpine.data("syncBranchLink", syncBranchLink({ store }));
  Alpine.data("syncPanel", syncPanel({ store, actions, now }));
  Alpine.data("syncFetch", syncFetch({ store, actions, now }));
  Alpine.data("syncPopover", syncPopover({ store, actions, now }));
}
