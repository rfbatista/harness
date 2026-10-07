// The wire format of internal/domain/session.go and ports.SessionChange, and
// its mapping to domain sessions. The only place that knows the JSON shape;
// a mismatch is a BAD_RESPONSE, so code past here can trust its data.

import { Codes, StructuredError } from "../../../shared/domain/errors.js";
import { CHECK_STATES, ROLES } from "../domain/channel.js";
import { STATUSES } from "../domain/session.js";

/** @returns {import("../domain/session.js").Session} */
export function toSession(dto) {
  if (!dto || typeof dto.id !== "string" || dto.id === "") bad("session without an id");
  if (!STATUSES.includes(dto.status)) bad(`session ${dto.id} has unknown status "${dto.status}"`);
  const updatedAt = new Date(dto.updated_at);
  if (Number.isNaN(updatedAt.getTime())) bad(`session ${dto.id} has an invalid updated_at`);
  const role = dto.role ?? "";
  if (!ROLES.includes(role)) bad(`session ${dto.id} has unknown role "${role}"`);

  return Object.freeze({
    id: dto.id,
    projectId: dto.project_id ?? "",
    ticketId: dto.ticket_id ?? "",
    repositoryId: dto.repository_id ?? "",
    task: dto.task ?? "",
    agentId: dto.agent_id ?? "",
    mode: dto.mode ?? "",
    status: dto.status,
    pendingApprovals: Number(dto.pending_approvals ?? 0),
    lastAction: dto.last_action ?? "",
    interactive: dto.interactive === true,
    runsOn: dto.runs_on ?? "",
    runnerHost: dto.runner_host ?? "",
    resumable: dto.resumable === true,
    resumeBlocked: typeof dto.resume_blocked === "string" ? dto.resume_blocked : "",
    role,
    architectSessionId: dto.architect_session_id ?? "",
    statusCheck: dto.status_check ? toStatusCheck(dto.status_check) : null,
    parentSessionId: dto.parent_session_id ?? "",
    branch: dto.branch ?? "",
    workspaceId: dto.workspace_id ?? "",
    workingDir: dto.working_dir ?? "",
    updatedAt,
  });
}

/**
 * A status check (domain.StatusCheck): on a delegate's session, in a
 * `status_check` feed message, and in POST /api/set_status_check's answer.
 * @returns {import("../domain/channel.js").StatusCheck}
 */
export function toStatusCheck(dto) {
  if (!dto || typeof dto.delegate_session_id !== "string" || dto.delegate_session_id === "") bad("status check without a delegate_session_id");
  const at = `status check on ${dto.delegate_session_id}`;
  if (!CHECK_STATES.includes(dto.state)) bad(`${at} has unknown state "${dto.state}"`);
  if (!Number.isInteger(dto.every_minutes)) bad(`${at} has no every_minutes`);
  return Object.freeze({
    taskId: dto.task_id ?? "",
    architectSessionId: dto.architect_session_id ?? "",
    delegateSessionId: dto.delegate_session_id,
    everyMinutes: dto.every_minutes,
    nextAt: optionalDate(dto.next_at, `${at}: next_at`),
    lastFiredAt: optionalDate(dto.last_fired_at, `${at}: last_fired_at`),
    firedCount: Number(dto.fired_count ?? 0),
    state: dto.state,
  });
}

/** An optional RFC 3339 time: null when absent, BAD_RESPONSE when unreadable. */
export function optionalDate(value, what) {
  if (value === undefined || value === null || value === "") return null;
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) bad(`${what} is not a time`);
  return d;
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
    repositoryNames: { ...(body.repository_names ?? {}) },
    documentTitles: { ...(body.document_titles ?? {}) },
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
    mode: req.mode || undefined,
    prompt: req.prompt || undefined,
    auto_accept: req.autoAccept,
    runs_on: "server",
    size: req.size,
    base_branch: req.baseBranch || undefined,
  };
}

/** POST /api/resume_interactive_session body: the web client resumes on the server's terminal host. */
export function toResumeBody(sessionId, size) {
  return { session_id: sessionId, runs_on: "server", size };
}

/** GET /api/list_branches → {"branches": [{name, remote, is_head}]} */
export function toBranches(body) {
  if (!body || !Array.isArray(body.branches)) bad("expected {branches: [...]}");
  return body.branches.map((b) => {
    if (typeof b?.name !== "string" || b.name === "") bad("branch without a name");
    return Object.freeze({ name: b.name, remote: b.remote === true, isHead: b.is_head === true });
  });
}

/** POST /api/start_interactive_session and /api/resume_interactive_session → {"session": {...}, "agent": {...}} */
export function toCreated(body) {
  if (!body?.session) bad("expected {session: {...}}");
  return toSession(body.session);
}

function bad(detail) {
  throw new StructuredError(Codes.BAD_RESPONSE, `Unexpected session data: ${detail}.`);
}
