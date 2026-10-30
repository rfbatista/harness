// PersonTerminalGateway over the harness HTTP API ("Contract: Harness server ↔
// Web UI — person's terminals"): open, list and close on /api, and the
// `person_terminal_ended` event on the session's feed. The terminal's own
// socket is the terminal gateway's, with person-terminals' path.

import { Codes, StructuredError } from "../../../shared/domain/errors.js";
import { optionalDate } from "./dto.js";

/** @returns {import("../domain/ports.js").PersonTerminal} */
export function toPersonTerminal(dto) {
  if (!dto || typeof dto.id !== "string" || dto.id === "") {
    throw new StructuredError(Codes.BAD_RESPONSE, "Unexpected terminal data: a terminal without an id.");
  }
  return Object.freeze({
    id: dto.id,
    sessionId: String(dto.session_id ?? ""),
    root: String(dto.root ?? ""),
    dir: String(dto.dir ?? ""),
    label: String(dto.label ?? dto.dir ?? ""),
    openedAt: optionalDate(dto.opened_at, "terminal: opened_at") ?? new Date(0),
  });
}

/**
 * @param {import("../../../shared/infrastructure/api.js").ApiClient} api
 * @param {import("../../../shared/infrastructure/feed.js").Feed} feed
 * @returns {import("../domain/ports.js").PersonTerminalGateway}
 */
export function personTerminalsGateway(api, feed) {
  const of = (sessionId) => `/sessions/${encodeURIComponent(sessionId)}/person-terminals`;
  return {
    async list(sessionId, signal) {
      const rows = await api.get(of(sessionId), {}, signal);
      if (!Array.isArray(rows)) throw new StructuredError(Codes.BAD_RESPONSE, "Unexpected terminal data: expected a list.");
      return rows.map(toPersonTerminal);
    },

    async open(sessionId, { root, dir, cols, rows }) {
      return toPersonTerminal(await api.post(of(sessionId), { root, dir, cols, rows }));
    },

    async close(id) {
      await api.del(`/person-terminals/${encodeURIComponent(id)}`);
    },

    followEnded(sessionId, onEnded, onStatus) {
      return feed.follow(`/sessions/${encodeURIComponent(sessionId)}/events`, {
        onMessage: (dto) => {
          if (dto?.type !== "person_terminal_ended" || typeof dto.id !== "string" || dto.id === "") return;
          onEnded({ id: dto.id, sessionId: String(dto.session_id ?? sessionId), reason: String(dto.reason ?? ""), message: String(dto.message ?? "") });
        },
        onStatus,
      });
    },
  };
}
