// AgentBoardGateway over the harness HTTP API (GET /api/agent_board) and the
// live session feed (/live_sessions/events, followed on the page event stream).

import { toEvent, toSeed } from "./dto.js";

export const FEED_PATH = "/live_sessions/events";

/**
 * @param {import("../../../shared/infrastructure/api.js").ApiClient} api
 * @param {import("../../../shared/infrastructure/feed.js").Feed} events
 * @returns {import("../domain/ports.js").AgentBoardGateway}
 */
export function agentBoardGateway(api, events) {
  return {
    decodeSeed: toSeed,

    async readSeed() {
      return toSeed(await api.get("/agent_board"));
    },

    follow(onEvent, onStatus = () => {}) {
      return events.follow(FEED_PATH, {
        onMessage(data) {
          let event;
          try {
            event = toEvent(data);
          } catch {
            return; // a malformed event is skipped; the next one's seq shows the gap
          }
          if (event) onEvent(event);
        },
        onStatus,
      });
    },
  };
}
