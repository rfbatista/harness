// SessionGateway over the harness HTTP API and its SSE feed.

import { toChange, toSeed, toSessionList } from "./dto.js";

/**
 * @param {import("../../../shared/infrastructure/api.js").ApiClient} api
 * @param {import("../../../shared/infrastructure/feed.js").Feed} feed
 * @returns {import("../domain/ports.js").SessionGateway}
 */
export function sessionsGateway(api, feed) {
  return {
    decodeSeed: toSeed,

    async list(projectId, signal) {
      return toSessionList(await api.get("/sessions", { project_id: projectId }, signal));
    },

    async send(sessionId, text) {
      await api.post(`/sessions/${encodeURIComponent(sessionId)}/messages`, { text });
    },

    async stop(sessionId) {
      await api.post(`/sessions/${encodeURIComponent(sessionId)}/stop`);
    },

    follow(projectId, onChange, onStatus) {
      return feed.follow(`/events?project_id=${encodeURIComponent(projectId)}`, {
        onMessage: (dto) => {
          try {
            onChange(toChange(dto));
          } catch {
            // A change we cannot read is dropped; the next resync corrects the list.
          }
        },
        onStatus,
      });
    },
  };
}
