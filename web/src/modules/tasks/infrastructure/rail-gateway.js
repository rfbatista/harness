// RailGateway over the harness HTTP API: GET /api/sessions and the project's
// session feed (/api/events, server-sent events).

import { Codes, StructuredError } from "../../../shared/domain/errors.js";

/** A session on the wire (internal/domain/session.go), as much as the rail reads. */
export function toRailSession(dto) {
  if (!dto || typeof dto.id !== "string" || dto.id === "") bad("session without an id");
  if (typeof dto.status !== "string") bad(`session ${dto.id} without a status`);
  return Object.freeze({
    id: dto.id,
    ticketId: dto.ticket_id ?? "",
    status: dto.status,
    pendingApprovals: Number(dto.pending_approvals ?? 0),
  });
}

/** A feed message (ports.SessionChange). */
export function toRailChange(dto) {
  if (dto?.deleted) {
    if (typeof dto.session?.id !== "string") bad("deleted change without a session id");
    return { kind: "deleted", id: dto.session.id };
  }
  return { kind: "upsert", session: toRailSession(dto?.session) };
}

/**
 * @param {import("../../../shared/infrastructure/api.js").ApiClient} api
 * @param {import("../../../shared/infrastructure/feed.js").Feed} feed
 * @returns {import("../domain/ports.js").RailGateway}
 */
export function railGateway(api, feed) {
  return {
    decodeSeed(seed) {
      if (!seed || typeof seed.project_id !== "string" || !Array.isArray(seed.sessions)) bad("rail seed without project_id and sessions");
      return { projectId: seed.project_id, sessions: seed.sessions.map(toRailSession) };
    },
    async listSessions(projectId) {
      const body = await api.get("/sessions", { project_id: projectId });
      if (!Array.isArray(body?.sessions)) bad("expected {sessions: [...]}");
      return body.sessions.map(toRailSession);
    },
    follow(projectId, onChange, onStatus) {
      return feed.follow(`/events?project_id=${encodeURIComponent(projectId)}`, {
        onMessage: (dto) => {
          try {
            onChange(toRailChange(dto));
          } catch {
            // A change it cannot read is dropped; the next resync corrects the rail.
          }
        },
        onStatus,
      });
    },
  };
}

function bad(detail) {
  throw new StructuredError(Codes.BAD_RESPONSE, `Unexpected session data: ${detail}.`);
}
