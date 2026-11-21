// The composition root of the server render: the top bar from the island props
// alone. Its adapters are in memory, so nothing touches the network or the DOM
// (gohydrate renders it in goja).

import { memoryPageOpener } from "../features/navigation/infrastructure/memory-page-opener";
import { memoryNavigator } from "../features/notifications/infrastructure/memory-navigator";
import { memoryNotificationsSource } from "../features/notifications/infrastructure/memory-notifications-source";
import { buildTopBar } from "./top-bar-presenters";
import { TopBarView } from "./top-bar-view";

/** The top bar island for the server render: `props` is the top-bar model. */
export function ServerTopBar({ props }: { props: unknown }) {
  const { presenters } = buildTopBar(props, { pageOpener: memoryPageOpener(), navigator: memoryNavigator(), notifications: memoryNotificationsSource() });
  return <TopBarView presenters={presenters} />;
}
