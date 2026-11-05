// The stream bar's connection indicator. Pages dispatch `feed-status` from
// their feed; this listens on window, since the bar is not their ancestor.
//
//   <span class="[ status ]" x-data="streamStatus"
//         @feed-status.window="update" :data-state="state" x-text="label"></span>

import { FeedStatus } from "../../domain/feed.js";
import { feedStatusView } from "../feedStatus.js";

// The connection as it was last reported: a stream bar swapped in with a page
// starts from it, since the feed that reports it stays open across pages.
let lastStatus = FeedStatus.CONNECTING;

export const streamStatus = () => () => ({
  status: lastStatus,

  get state() {
    return feedStatusView(this.status).state;
  },
  get label() {
    return feedStatusView(this.status).label;
  },

  /** @param {CustomEvent<string>} event */
  update(event) {
    const next = event.detail;
    // resynced is a moment, not a state: the feed is live again.
    this.status = lastStatus = next === FeedStatus.RESYNCED ? FeedStatus.LIVE : next;
  },
});
