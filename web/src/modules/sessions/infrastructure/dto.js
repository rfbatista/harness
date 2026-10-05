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
    ticketId: dto.ticket_id ?? "",
    repositoryId: dto.repository_id ?? "",
    task: dto.task ?? "",
    agentId: dto.agent_id ?? "",
    status: dto.status,
    pendingApprovals: Number(dto.pending_approvals ?? 0),
    lastAction: dto.last_action ?? "",
    interactive: dto.interactive === true,
    runsOn: dto.runs_on ?? "",
    runnerHost: dto.runner_host ?? "",
    updatedAt,
  });
}

/** GET /api/sessions → {"sessions": [...]} */
export function toSessionList(body) {
  if (!body || !Array.isArray(body.sessions)) bad("expected {sessions: [...]}");
  return body.sessions.map(toSession);
}

/** A task page's seed: {"project_id": "...", "ticket_id": "...", "sessions": [...]} */
export function toSeed(body) {
  if (!body || typeof body.project_id !== "string") bad("seed without a project_id");
  return {
    projectId: body.project_id,
    ticketId: body.ticket_id ?? "",
    sessions: toSessionList(body),
    agentNames: { ...(body.agent_names ?? {}) },
  };
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

/** POST /api/start_interactive_session body: always run on the server's terminal host. */
export function toStartBody(req) {
  return {
    project_id: req.projectId,
    ticket_id: req.ticketId,
    repository_id: req.repositoryId,
    agent_id: req.agentId || undefined,
    prompt: req.prompt || undefined,
    auto_accept: req.autoAccept,
    runs_on: "server",
    size: req.size,
    base_branch: req.baseBranch || undefined,
  };
}

/** GET /api/list_branches → {"branches": [{name, remote, is_head}]} */
export function toBranches(body) {
  if (!body || !Array.isArray(body.branches)) bad("expected {branches: [...]}");
  return body.branches.map((b) => {
    if (typeof b?.name !== "string" || b.name === "") bad("branch without a name");
    return Object.freeze({ name: b.name, remote: b.remote === true, isHead: b.is_head === true });
  });
}

/** POST /api/start_interactive_session → {"session": {...}, "agent": {...}} */
export function toCreated(body) {
  if (!body?.session) bad("expected {session: {...}}");
  return toSession(body.session);
}

function bad(detail) {
  throw new StructuredError(Codes.BAD_RESPONSE, `Unexpected session data: ${detail}.`);
}
