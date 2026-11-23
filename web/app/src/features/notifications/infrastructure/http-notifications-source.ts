// NotificationsSource over the harness HTTP API (the notifications contract's /api/notifications routes).

import { ApiError, type ApiClient } from "../../../shared/infrastructure/api-client";
import { NotificationsSourceError, type NotificationsSource, type NotificationsSourceErrorCode } from "../application/ports";
import type { Feed } from "../../../shared/kernel/feed";
import { DtoError } from "../../../shared/infrastructure/dto/dto-error";
import { ChangeEventDto, NotificationPageDto, ReadAnswerDto } from "./dto/notification-page-dto";
import { toChange, toPage, toReadRequestDto } from "./notification-mapper";

/** The global notifications stream (GET /api/notifications/events) as the page event stream names it. */
export const NOTIFICATIONS_STREAM = "/notifications/events";

const KNOWN: readonly string[] = ["INVALID_INPUT", "NOTIFICATION_NOT_FOUND", "NETWORK_ERROR", "BAD_RESPONSE"];

/** Every adapter call fails with a NotificationsSourceError. */
export async function mapErrors<T>(call: () => Promise<T>): Promise<T> {
  try {
    return await call();
  } catch (err) {
    if (err instanceof DtoError) throw new NotificationsSourceError("BAD_RESPONSE", err.message);
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
      return mapErrors(async () => toPage(NotificationPageDto.fromJSON(await api.get("/notifications", query, signal))));
    },

    markRead: (request) => mapErrors(async () => toChange(ReadAnswerDto.fromJSON(await api.post("/notifications/read", toReadRequestDto(request))))),

    follow: (onChange, onStatus) =>
      feed.follow(NOTIFICATIONS_STREAM, {
        onMessage(data) {
          try {
            onChange(toChange(ChangeEventDto.fromJSON(data)));
          } catch (err) {
            // A message that is not a valid change event is dropped; the next resync corrects the list. Anything else is a bug.
            if (!(err instanceof DtoError)) throw err;
          }
        },
        onStatus,
      }),
  };
}
