// What a failed load or action, and a dropped stream, read as: plain sentences, no code and no developer message.

import { NotificationsSourceError } from "../../application/ports";
import type { FeedStatus } from "../../domain/feed-status";

export function inboxErrorMessage(err: unknown): string {
  switch (err instanceof NotificationsSourceError ? err.code : "") {
    case "NETWORK_ERROR":
      return "The notifications could not be loaded. The harness server did not answer.";
    case "BAD_RESPONSE":
      return "The notifications could not be read. Reload; if it stays, check the harness server log.";
    default:
      return "The notifications could not be loaded.";
  }
}

/** The line a dropped stream shows; "" while the stream is fine. */
export function streamNotice(status: FeedStatus): string {
  return status === "paused" ? "Live updates are paused. Reconnecting; Reload shows what is there now." : "";
}
