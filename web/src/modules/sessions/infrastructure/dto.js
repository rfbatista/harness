// The wire format of internal/domain/session.go and ports.SessionChange, and
// its mapping to domain sessions. The only place that knows the JSON shape;
// a mismatch is a BAD_RESPONSE, so code past here can trust its data.

import { Codes, StructuredError } from "../../../shared/domain/errors.js";
import { STATUSES } from "../domain/session.js";

/** @returns {import("../domain/session.js").Session} */
export function toSession(dto) {
  if (!dto || typeof dto.id !== "string" || dto.id === "") bad("session without an id");
  if (!STATUSES.includes(dto.status)) bad(`session ${dto.id} has unknown status "${dto.status}"`);
  const updatedAt = new Date(dto.updated_at);
  if (Number.isNaN(updatedAt.getTime())) bad(`session ${dto.id} has an invalid updated_at`);

  return Object.freeze({
    id: dto.id,
    projectId: dto.project_id ?? "",
    task: dto.task ?? "",
    agentId: dto.agent_id ?? "",
    status: dto.status,
    pendingApprovals: Number(dto.pending_approvals ?? 0),
    lastAction: dto.last_action ?? "",
    updatedAt,
  });
}

/** GET /api/sessions → {"sessions": [...]} */
export function toSessionList(body) {
  if (!body || !Array.isArray(body.sessions)) bad("expected {sessions: [...]}");
  return body.sessions.map(toSession);
}

/** The sessions page seed: {"project_id": "...", "sessions": [...]} */
export function toSeed(body) {
  if (!body || typeof body.project_id !== "string") bad("seed without a project_id");
  return { projectId: body.project_id, sessions: toSessionList(body) };
}

/** One `data:` line of GET /api/events: {"session": {...}, "deleted": true?} */
export function toChange(dto) {
  if (dto?.deleted) {
    const id = dto.session?.id;
    if (typeof id !== "string") bad("deleted change without a session id");
    return { kind: "deleted", id };
  }
  return { kind: "upsert", session: toSession(dto?.session) };
}

function bad(detail) {
  throw new StructuredError(Codes.BAD_RESPONSE, `Unexpected session data: ${detail}.`);
}
