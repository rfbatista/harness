// The wire format of notifications (the Web UI contract: GET
// /api/notifications, POST /api/notifications/read and the
// /api/notifications/events stream) and its mapping to domain notifications.

import { Codes, StructuredError } from "../../../shared/domain/errors.js";
import { INTERRUPTED_STATES, KINDS, LEVELS, SOURCES } from "../domain/notification.js";

/** @returns {import("../domain/notification.js").Notification} */
export function toNotification(dto) {
  if (!dto || typeof dto.id !== "string" || dto.id === "") bad("notification without an id");
  const at = `notification ${dto.id}`;
  if (!SOURCES.includes(dto.source)) bad(`${at} has unknown source "${dto.source}"`);
  if (!KINDS.includes(dto.kind)) bad(`${at} has unknown kind "${dto.kind}"`);
  if (!LEVELS.includes(dto.level)) bad(`${at} has unknown level "${dto.level}"`);
  // A path on this web UI only: "//host" is another origin.
  if (typeof dto.link !== "string" || !dto.link.startsWith("/") || dto.link.startsWith("//")) bad(`${at} has a link off the web UI`);
  const createdAt = date(dto.created_at, `${at}: created_at`);
  if (!createdAt) bad(`${at} has no created_at`);
  return Object.freeze({
    id: dto.id,
    projectId: dto.project_id ?? "",
    projectName: dto.project_name ?? "",
    taskId: dto.task_id ?? "",
    taskTitle: dto.task_title ?? "",
    sessionId: dto.session_id ?? "",
    source: dto.source,
    kind: dto.kind,
    level: dto.level,
    title: dto.title ?? "",
    body: dto.body ?? "",
    link: dto.link,
    createdAt,
    readAt: date(dto.read_at, `${at}: read_at`),
    resolvedAt: date(dto.resolved_at, `${at}: resolved_at`),
    interruptedSessions: (dto.interrupted_sessions ?? []).map((s) => toInterruptedSession(s, at)),
  });
}

function toInterruptedSession(dto, at) {
  if (!dto || typeof dto.session_id !== "string" || dto.session_id === "") bad(`${at} lists a session without an id`);
  if (!INTERRUPTED_STATES.includes(dto.state)) bad(`${at} lists session ${dto.session_id} in unknown state "${dto.state}"`);
  if (typeof dto.link !== "string" || !dto.link.startsWith("/") || dto.link.startsWith("//")) bad(`${at} lists session ${dto.session_id} with a link off the web UI`);
  return Object.freeze({
    sessionId: dto.session_id,
    projectId: dto.project_id ?? "",
    taskId: dto.task_id ?? "",
    taskTitle: dto.task_title ?? "",
    agentName: dto.agent_name ?? "",
    link: dto.link,
    state: dto.state,
    reason: dto.reason ?? "",
  });
}

/** GET /api/notifications → {notifications, unread_count, next_before?} */
export function toPage(body) {
  if (!body || !Array.isArray(body.notifications)) bad("expected {notifications: [...], unread_count}");
  return {
    notifications: body.notifications.map(toNotification),
    unreadCount: count(body.unread_count),
    nextBefore: typeof body.next_before === "string" ? body.next_before : "",
  };
}

/** POST /api/notifications/read → {notifications: [changed…], unread_count} */
export function toReadAnswer(body) {
  if (!body || !Array.isArray(body.notifications)) bad("expected {notifications: [...], unread_count}");
  return { notifications: body.notifications.map(toNotification), unreadCount: count(body.unread_count) };
}

/** One stream message: {changed: [...], unread_count}. */
export function toStreamChange(body) {
  if (!body || !Array.isArray(body.changed)) bad("expected {changed: [...], unread_count}");
  return { notifications: body.changed.map(toNotification), unreadCount: count(body.unread_count) };
}

/** POST /api/notifications/{id}/resume_interrupted → {notification, results: [{session_id, outcome, reason?}]} */
export function toResumeAnswer(body) {
  if (!body || !body.notification || !Array.isArray(body.results)) bad("expected {notification, results}");
  return {
    notification: toNotification(body.notification),
    results: body.results.map((r) => Object.freeze({ sessionId: String(r.session_id ?? ""), outcome: r.outcome, reason: r.reason ?? "" })),
  };
}

function count(v) {
  if (!Number.isInteger(v) || v < 0) bad("unread_count is not a count");
  return v;
}

function date(value, what) {
  if (value === undefined || value === null || value === "") return null;
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) bad(`${what} is not a time`);
  return d;
}

function bad(detail) {
  throw new StructuredError(Codes.BAD_RESPONSE, `Unexpected notification: ${detail}.`);
}
