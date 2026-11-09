// InterviewGateway over the harness HTTP API: GET /api/sessions/{id}/interviews
// and the session's interview feed (/sessions/{id}/interviews/events) on the page stream.

import { toInterviewEvent, toInterviewList } from "./interviews-dto.js";

/**
 * @param {import("../../../shared/infrastructure/api.js").ApiClient} api
 * @param {import("../../../shared/infrastructure/feed.js").Feed} feed
 * @returns {import("../domain/ports.js").InterviewGateway}
 */
export function interviewsGateway(api, feed) {
  const path = (sessionId) => `/sessions/${encodeURIComponent(sessionId)}/interviews`;
  return {
    async list(sessionId, signal) {
      return toInterviewList(await api.get(path(sessionId), {}, signal));
    },

    follow(sessionId, onEvent, onStatus) {
      return feed.follow(`${path(sessionId)}/events`, {
        onMessage: (dto) => {
          let event;
          try {
            event = toInterviewEvent(dto);
          } catch {
            return; // one we cannot read is dropped; the next event shows the gap and the list is read again
          }
          onEvent(event);
        },
        onStatus,
      });
    },
  };
}
