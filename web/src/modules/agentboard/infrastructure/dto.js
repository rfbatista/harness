// The wire format of the Agent board seed and the live session feed
// ("Contract: harness server ↔ web UI — the agent board seed and the live
// session feed") and its mapping to the domain's camelCase.

import { Codes, StructuredError } from "../../../shared/domain/errors.js";

const bad = (why) => {
  throw new StructuredError(Codes.BAD_RESPONSE, `the agent board answered something unexpected: ${why}`, 0);
};

const time = (value) => {
  const at = new Date(value);
  return Number.isNaN(at.getTime()) ? null : at;
};

const str = (value) => (typeof value === "string" ? value : "");

/** @returns {import("../domain/board.js").SessionLine} */
export function toSessionLine(dto) {
  if (!dto || typeof dto.session_id !== "string" || typeof dto.status !== "string") bad("a session line without an id or a status");
  return Object.freeze({
    sessionId: dto.session_id,
    agentName: str(dto.agent_name) || "claude",
    role: str(dto.role) || "peer",
    status: dto.status,
    statusSince: time(dto.status_since) ?? new Date(0),
    waitingFor: typeof dto.waiting_for === "string" && dto.waiting_for !== "" ? dto.waiting_for : null,
    mode: str(dto.mode),
    model: str(dto.model),
    openInterviewCount: Number.isInteger(dto.open_interview_count) ? dto.open_interview_count : 0,
    openQuestionCount: Number.isInteger(dto.open_question_count) ? dto.open_question_count : 0,
  });
}

/** @returns {import("../domain/board.js").Card} */
export function toCard(dto) {
  if (!dto || typeof dto.task_id !== "string" || !Array.isArray(dto.sessions)) bad("a card without a task or sessions");
  return Object.freeze({
    taskId: dto.task_id,
    projectId: str(dto.project_id),
    taskTitle: str(dto.task_title),
    taskStatus: str(dto.task_status),
    featureId: typeof dto.feature_id === "string" ? dto.feature_id : null,
    featureTitle: typeof dto.feature_title === "string" ? dto.feature_title : null,
    needsYou: dto.needs_you === true,
    needsYouSince: dto.needs_you_since ? time(dto.needs_you_since) : null,
    lastStatusChangeAt: time(dto.last_status_change_at) ?? new Date(0),
    sessions: Object.freeze(dto.sessions.map(toSessionLine)),
  });
}

/** GET /api/agent_board, and the page's seed. */
export function toSeed(body) {
  if (!body || typeof body.seq !== "number" || !Array.isArray(body.projects)) bad("expected {seq, projects: [...]}");
  return Object.freeze({
    seq: body.seq,
    generatedAt: time(body.generated_at),
    projectCount: Number(body.project_count) || 0,
    quietProjects: Number(body.quiet_projects) || 0,
    projects: Object.freeze(
      body.projects.map((p) =>
        Object.freeze({
          projectId: str(p?.project_id),
          projectName: str(p?.project_name),
          cards: Object.freeze((p?.cards ?? []).map(toCard)),
        }),
      ),
    ),
  });
}

/** One event of /live_sessions/events; null when it is not one this page reads. */
export function toEvent(dto) {
  if (!dto || typeof dto.seq !== "number" || typeof dto.kind !== "string") return null;
  return Object.freeze({
    seq: dto.seq,
    kind: dto.kind,
    projectId: str(dto.project_id),
    projectName: str(dto.project_name),
    taskId: str(dto.task_id),
    sessionId: str(dto.session_id),
    ended: str(dto.ended),
    session: dto.session ? toSessionLine(dto.session) : null,
    card: dto.card ? toCard(dto.card) : null,
  });
}
