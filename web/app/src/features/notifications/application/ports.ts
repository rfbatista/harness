// What the notifications feature needs from the outside world.

import type { NotificationPage } from "../domain/notification";

export type NotificationsSourceErrorCode = "INVALID_INPUT" | "NOTIFICATION_NOT_FOUND" | "NETWORK_ERROR" | "BAD_RESPONSE" | "UNKNOWN";

export class NotificationsSourceError extends Error {
  readonly code: NotificationsSourceErrorCode;

  constructor(code: NotificationsSourceErrorCode, message: string) {
    super(message);
    this.name = "NotificationsSourceError";
    this.code = code;
  }
}

export interface ListQuery {
  /** Only the unread ones. */
  readonly unread?: boolean;
  /** A page's nextBefore. */
  readonly before?: string;
  /** 1..200; the server's default is 50. */
  readonly limit?: number;
}

/** The person's notifications (GET /api/notifications). Rejects with a NotificationsSourceError. */
export interface NotificationsSource {
  /** Newest first, across every project. `unreadCount` is the global count whatever the filter. */
  list(query: ListQuery, signal?: AbortSignal): Promise<NotificationPage>;
}
