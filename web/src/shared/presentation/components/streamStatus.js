// The stream bar's connection indicator. Pages dispatch `feed-status` from
// their feed; this listens on window, since the bar is not their ancestor.
//
//   <span class="[ status ]" x-data="streamStatus"
//         @feed-status.window="update" :data-state="state" x-text="label"></span>

import { FeedStatus } from "../../domain/feed.js";

const VIEW = {
  [FeedStatus.CONNECTING]: { state: "reconnecting", label: "connecting" },
  [FeedStatus.LIVE]: { state: "live", label: "live" },
  [FeedStatus.PAUSED]: { state: "reconnecting", label: "live updates paused · retrying" },
};

export const streamStatus = () => () => ({
  status: FeedStatus.CONNECTING,

  get state() {
    return VIEW[this.status].state;
  },
  get label() {
    return VIEW[this.status].label;
  },

  /** @param {CustomEvent<string>} event */
  update(event) {
    const next = event.detail;
    // resynced is a moment, not a state: the feed is live again.
    this.status = next === FeedStatus.RESYNCED ? FeedStatus.LIVE : next;
  },
});
