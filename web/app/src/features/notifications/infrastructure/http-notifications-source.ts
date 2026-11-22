// NotificationsSource over the harness HTTP API (the notifications contract's /api/notifications routes).

import { ApiError, type ApiClient } from "../../../shared/infrastructure/api-client";
import { NotificationsSourceError, type NotificationsSource, type NotificationsSourceErrorCode } from "../application/ports";
import type { Feed } from "../../../shared/kernel/feed";
import { toPage, toReadAnswer, toStreamChange } from "./dto";

/** The global notifications stream (GET /api/notifications/events) as the page event stream names it. */
export const NOTIFICATIONS_STREAM = "/notifications/events";

const KNOWN: readonly string[] = ["INVALID_INPUT", "NOTIFICATION_NOT_FOUND", "NETWORK_ERROR", "BAD_RESPONSE"];

/** Every adapter call fails with a NotificationsSourceError. */
export async function mapErrors<T>(call: () => Promise<T>): Promise<T> {
  try {
    return await call();
  } catch (err) {
    if (err instanceof ApiError) throw new NotificationsSourceError(KNOWN.includes(err.code) ? (err.code as NotificationsSourceErrorCode) : "UNKNOWN", err.message);
    throw err;
  }
}

export function httpNotificationsSource(api: ApiClient, feed: Feed): NotificationsSource {
  return {
    list: ({ unread, before, limit } = {}, signal) => {
      const query: Record<string, string> = {};
      if (unread) query.unread = "true";
      if (before) query.before = before;
      if (limit !== undefined) query.limit = String(limit);
      return mapErrors(async () => toPage(await api.get<unknown>("/notifications", query, signal)));
    },

    markRead: (request) => mapErrors(async () => toReadAnswer(await api.post<unknown>("/notifications/read", "ids" in request ? { ids: request.ids } : "all" in request ? { all: true } : {}))),

    follow: (onChange, onStatus) =>
      feed.follow(NOTIFICATIONS_STREAM, {
        onMessage(data) {
          try {
            onChange(toStreamChange(data));
          } catch {
            // A message that cannot be read is dropped; the next resync corrects the list.
          }
        },
        onStatus,
      }),
  };
}
