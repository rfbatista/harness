// What the notifications feature needs from the outside world.

import type { FeedStatus } from "../domain/feed-status";
import type { Change, NotificationPage } from "../domain/notification";

export type NotificationsSourceErrorCode = "INVALID_INPUT" | "NOTIFICATION_NOT_FOUND" | "NETWORK_ERROR" | "BAD_RESPONSE" | "UNKNOWN";

export class NotificationsSourceError extends Error {
  readonly code: NotificationsSourceErrorCode;

  constructor(code: NotificationsSourceErrorCode, message: string) {
    super(message);
    this.name = "NotificationsSourceError";
    this.code = code;
  }
}

/** Marks the ids read, or every unread notification (`all`). */
export type ReadRequest = { readonly ids: readonly string[] } | { readonly all: true };

export interface ListQuery {
  /** Only the unread ones. */
  readonly unread?: boolean;
  /** Only the open causes (level action, not settled), however old. */
  readonly open?: boolean;
  /** A page's nextBefore. */
  readonly before?: string;
  /** 1..200; the server's default is 50. */
  readonly limit?: number;
}

/** The person's notifications (GET /api/notifications). Rejects with a NotificationsSourceError. */
export interface NotificationsSource {
  /** Newest first, across every project. `unreadCount` is the global count whatever the filter. */
  list(query: ListQuery, signal?: AbortSignal): Promise<NotificationPage>;

  /**
   * Marks read. Answers with the notifications it changed and the global count; ids already read are not an
   * error. Rejects with NOTIFICATION_NOT_FOUND for an unknown id (and marks nothing), INVALID_INPUT for neither.
   */
  markRead(request: ReadRequest): Promise<Change>;

  /**
   * Every notification created, read or resolved, in every project, with the
   * global count as of that change, in order, until the returned function is
   * called. The person's own reads echo here. `onStatus` tells whether the
   * stream is up: "resynced" means changes were missed while it was down.
   */
  follow(onChange: (change: Change) => void, onStatus: (status: FeedStatus) => void): () => void;
}

/** Takes the person to a page (a full page load). */
export interface Navigator {
  open(url: string): void;
}
