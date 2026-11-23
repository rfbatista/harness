import type { NotificationPage } from "../domain/notification";
import { PANEL_LIMIT } from "./inbox";
import type { NotificationsSource } from "./ports";

/**
 * What the panel shows: the latest notifications, all or only the unread, and
 * the open causes however old (ADR 0096), read together. Needs you leads the panel.
 */
export async function loadPanel(source: NotificationsSource, unread: boolean, signal?: AbortSignal): Promise<{ latest: NotificationPage; open: NotificationPage }> {
  const [latest, open] = await Promise.all([source.list({ unread, limit: PANEL_LIMIT }, signal), source.list({ unread, open: true, limit: PANEL_LIMIT }, signal)]);
  return { latest, open };
}
