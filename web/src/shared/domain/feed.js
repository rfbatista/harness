// The state of a live feed, as the stream bar shows it.

/** @typedef {"connecting"|"live"|"paused"|"resynced"} FeedStatus */

export const FeedStatus = Object.freeze({
  /** Opening the stream for the first time. */
  CONNECTING: "connecting",
  /** Connected; changes arrive as they happen. */
  LIVE: "live",
  /** Dropped; retrying with backoff. What is on screen may be stale. */
  PAUSED: "paused",
  /** Reconnected after a drop; changes were missed, so lists must reload. */
  RESYNCED: "resynced",
});
