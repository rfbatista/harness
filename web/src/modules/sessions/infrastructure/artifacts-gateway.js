// ArtifactGateway over the harness HTTP API: the artifact list route and the
// `artifact` events on a session's SSE stream (the one interactive sessions
// already use for status and done).

import { toArtifactEvent, toArtifactList } from "./artifact-dto.js";

/**
 * @param {import("../../../shared/infrastructure/api.js").ApiClient} api
 * @param {import("../../../shared/infrastructure/feed.js").Feed} feed
 * @param {string} [base]  the API prefix the browser loads artifact bytes from
 * @returns {import("../domain/ports.js").ArtifactGateway}
 */
export function artifactsGateway(api, feed, base = "/api") {
  return {
    async list(sessionId, signal) {
      return toArtifactList(await api.get("/artifacts", { session_id: sessionId }, signal), base);
    },

    follow(sessionId, onEvent, onStatus) {
      return feed.follow(`/sessions/${encodeURIComponent(sessionId)}/events`, {
        onMessage: (dto) => {
          let event;
          try {
            event = toArtifactEvent(dto, base);
          } catch {
            return; // an event we cannot read is dropped; the next resync corrects the list
          }
          if (event) onEvent(event);
        },
        onStatus,
      });
    },
  };
}
