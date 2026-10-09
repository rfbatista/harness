// NotificationGateway over the harness HTTP API (the Web UI contract's
// /api/notifications routes) and the global notifications stream.

import { toPage, toReadAnswer, toStreamChange } from "./dto.js";

/**
 * @param {import("../../../shared/infrastructure/api.js").ApiClient} api
 * @param {import("../../../shared/infrastructure/feed.js").Feed} feed
 * @returns {import("../domain/ports.js").NotificationGateway}
 */
export function notificationsGateway(api, feed) {
  return {
    async list({ projectId, unread, before, limit } = {}, signal) {
      const query = {
        project_id: projectId,
        unread: unread ? "true" : undefined,
        before,
        limit: limit === undefined ? undefined : String(limit),
      };
      return toPage(await api.get("/notifications", query, signal));
    },

    async markRead(request) {
      const body = "ids" in request ? { ids: request.ids } : request.all === true ? { all: true, project_id: request.projectId || undefined } : {};
      return toReadAnswer(await api.post("/notifications/read", body));
    },

    follow(onChange, onStatus) {
      return feed.follow("/notifications/events", {
        onMessage: (dto) => {
          let change;
          try {
            change = toStreamChange(dto);
          } catch {
            return; // a message we cannot read is dropped; the next resync corrects the list
          }
          onChange(change);
        },
        onStatus,
      });
    },
  };
}
