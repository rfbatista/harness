// A feed's status as a .status[data-state] and its word. The stream bar and
// the Design tab's bar say the same thing in the same words.

import { FeedStatus } from "../domain/feed.js";

const VIEW = {
  [FeedStatus.CONNECTING]: { state: "reconnecting", label: "connecting" },
  [FeedStatus.LIVE]: { state: "live", label: "live" },
  [FeedStatus.PAUSED]: { state: "reconnecting", label: "live updates paused · retrying" },
};

/** resynced is a moment, not a state: the feed is live again. */
export function feedStatusView(status) {
  return VIEW[status === FeedStatus.RESYNCED ? FeedStatus.LIVE : status] ?? VIEW[FeedStatus.CONNECTING];
}
