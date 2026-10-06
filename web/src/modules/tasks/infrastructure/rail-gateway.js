// RailGateway over the harness HTTP API: GET /api/sessions, GET
// /api/list_tickets and the project's feed (/api/events, server-sent events),
// which carries session and ticket changes.

import { Codes, StructuredError } from "../../../shared/domain/errors.js";
import { toTask, toTasks } from "./dto.js";

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

/** A `session` feed message (ports.SessionChange). */
export function toRailChange(dto) {
  if (dto?.deleted) {
    if (typeof dto.session?.id !== "string") bad("deleted change without a session id");
    return { kind: "deleted", id: dto.session.id };
  }
  return { kind: "upsert", session: toRailSession(dto?.session) };
}

/** A `ticket` feed message (the SSE contract): the full ticket, or its id with deleted. */
export function toTaskChange(dto) {
  if (dto?.deleted) {
    if (typeof dto.ticket?.id !== "string") bad("deleted ticket without an id");
    return { kind: "task-deleted", id: dto.ticket.id };
  }
  return { kind: "task-upsert", task: toTask(dto?.ticket) };
}

/**
 * One feed message as the rail and the board see it, or null for a kind
 * they do not know or cannot read (the contract: consumers ignore kinds they
 * do not know; a change that cannot be read is corrected by the next resync).
 */
export function toProjectChange(dto) {
  try {
    if (dto && "ticket" in dto) return toTaskChange(dto);
    if (dto && "session" in dto) return toRailChange(dto);
  } catch {
    return null;
  }
  return null;
}

/**
 * @param {import("../../../shared/infrastructure/api.js").ApiClient} api
 * @param {import("../../../shared/infrastructure/feed.js").Feed} feed
 * @returns {import("../domain/ports.js").RailGateway}
 */
export function railGateway(api, feed) {
  return {
    decodeSeed(seed) {
      if (!seed || typeof seed.project_id !== "string" || !Array.isArray(seed.sessions) || !Array.isArray(seed.tasks)) {
        bad("rail seed without project_id, sessions and tasks");
      }
      return { projectId: seed.project_id, sessions: seed.sessions.map(toRailSession), tasks: toTasks(seed.tasks) };
    },
    async listSessions(projectId) {
      const body = await api.get("/sessions", { project_id: projectId });
      if (!Array.isArray(body?.sessions)) bad("expected {sessions: [...]}");
      return body.sessions.map(toRailSession);
    },
    async listTasks(projectId) {
      const body = await api.get("/list_tickets", { project_id: projectId });
      if (!Array.isArray(body?.tickets)) bad("expected {tickets: [...]}");
      return toTasks(body.tickets);
    },
    follow(projectId, onChange, onStatus) {
      return feed.follow(`/events?project_id=${encodeURIComponent(projectId)}`, {
        onMessage: (dto) => {
          const change = toProjectChange(dto);
          if (change) onChange(change);
        },
        onStatus,
      });
    },
  };
}

function bad(detail) {
  throw new StructuredError(Codes.BAD_RESPONSE, `Unexpected session data: ${detail}.`);
}
