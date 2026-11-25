// The composition root of the server render: the top bar from the island props
// alone. Its adapters are in memory, so nothing touches the network or the DOM
// (gohydrate renders it in goja).

import { memoryPageOpener } from "../features/navigation/infrastructure/memory-page-opener";
import { memoryNavigator } from "../features/notifications/infrastructure/memory-navigator";
import { memoryNotificationsSource } from "../features/notifications/infrastructure/memory-notifications-source";
import { OwnerDock } from "../features/product-owner/application/owner-dock";
import { memoryProductOwner } from "../features/product-owner/infrastructure/memory-product-owner";
import { memoryDockMemory, memoryNavigator as memoryOwnerNavigator, memoryScreen } from "../features/product-owner/testing/memory-dock-adapters";
import { memoryAnnouncer, memoryFeaturePrefs, memoryPageOpener as memoryRailOpener, memoryRailPrefs, manualScheduler } from "../features/rail/infrastructure/memory-ports";
import { memoryRailFeed, memoryRailSource } from "../features/rail/infrastructure/memory-rail";
import { RailView } from "../features/rail/presentation/views/rail-view";
import { buildRail } from "./rail-presenters";
import { buildTopBar } from "./top-bar-presenters";
import { TopBarView } from "./top-bar-view";

/** The top bar island for the server render: `props` is the top-bar model. */
export function ServerTopBar({ props }: { props: unknown }) {
  const { presenters } = buildTopBar(props, { pageOpener: memoryPageOpener(), navigator: memoryNavigator(), notifications: memoryNotificationsSource(), dock: serverDock() });
  return <TopBarView presenters={presenters} />;
}

/** The rail island for the server render: `props` is the rail seed and navigation. Nothing is followed and no timer runs. */
export function ServerRail({ props }: { props: unknown }) {
  const { presenter } = buildRail(props, { source: memoryRailSource(), feed: memoryRailFeed(), prefs: memoryRailPrefs(), features: memoryFeaturePrefs(), announcer: memoryAnnouncer(), opener: memoryRailOpener(), scheduler: manualScheduler() });
  return <RailView presenter={presenter} host={null} />;
}

/** The dock for the server render: nothing remembered, nothing to read, so the toggle renders closed and on. */
function serverDock(): OwnerDock {
  const owner = memoryProductOwner();
  return new OwnerDock({ gateway: owner, events: owner, memory: memoryDockMemory(), screen: memoryScreen(false), navigator: memoryOwnerNavigator() });
}
