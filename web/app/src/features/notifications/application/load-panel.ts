import type { NotificationPage } from "../domain/notification";
import { PANEL_LIMIT } from "./inbox";
import type { NotificationsSource } from "./ports";

/** The latest notifications the panel shows, all or only the unread. */
export function loadPanel(source: NotificationsSource, unread: boolean, signal?: AbortSignal): Promise<NotificationPage> {
  return source.list({ unread, limit: PANEL_LIMIT }, signal);
}
