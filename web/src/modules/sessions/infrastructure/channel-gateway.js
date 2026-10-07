// ChannelGateway over the harness HTTP API (the Web UI contract's
// GET /api/task_messages and POST /api/set_status_check) and the project feed.

import { toStatusCheck } from "./dto.js";
import { toChannelEvent, toTaskMessages } from "./channel-dto.js";

/**
 * @param {import("../../../shared/infrastructure/api.js").ApiClient} api
 * @param {import("../../../shared/infrastructure/feed.js").Feed} feed
 * @returns {import("../domain/ports.js").ChannelGateway}
 */
export function channelGateway(api, feed) {
  return {
    async listMessages({ ticketId, sessionId, since }, signal) {
      const query = { ticket_id: ticketId, session_id: sessionId, since: since?.toISOString() };
      return toTaskMessages(await api.get("/task_messages", query, signal));
    },

    async setStatusCheck(delegateSessionId, everyMinutes) {
      const body = await api.post("/set_status_check", { delegate_session_id: delegateSessionId, every_minutes: everyMinutes });
      return toStatusCheck(body?.status_check);
    },

    follow(projectId, onEvent, onStatus) {
      return feed.follow(`/events?project_id=${encodeURIComponent(projectId)}`, {
        onMessage: (dto) => {
          let event;
          try {
            event = toChannelEvent(dto);
          } catch {
            return; // a message we cannot read is dropped; the next resync corrects the list
          }
          if (event) onEvent(event);
        },
        onStatus,
      });
    },
  };
}
