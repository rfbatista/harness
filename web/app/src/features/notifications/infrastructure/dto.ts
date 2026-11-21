// The wire format of notifications (GET /api/notifications, POST
// /api/notifications/read and the /api/notifications/events stream) and its
// mapping to domain notifications.

import { NotificationsSourceError } from "../application/ports";
import { INTERRUPTED_STATES, KINDS, LEVELS, SOURCES, type Change, type InterruptedSession, type Notification, type NotificationPage } from "../domain/notification";

type Wire = Record<string, unknown>;

const isRecord = (value: unknown): value is Wire => typeof value === "object" && value !== null && !Array.isArray(value);
const text = (value: unknown): string => (typeof value === "string" ? value : "");
// A path on this web UI only: "//host" is another origin.
const isLocalPath = (value: unknown): value is string => typeof value === "string" && value.startsWith("/") && !value.startsWith("//");

export function toNotification(dto: unknown): Notification {
  if (!isRecord(dto) || typeof dto.id !== "string" || dto.id === "") return bad("notification without an id");
  const at = `notification ${dto.id}`;
  if (!SOURCES.includes(dto.source as never)) return bad(`${at} has unknown source "${String(dto.source)}"`);
  if (!KINDS.includes(dto.kind as never)) return bad(`${at} has unknown kind "${String(dto.kind)}"`);
  if (!LEVELS.includes(dto.level as never)) return bad(`${at} has unknown level "${String(dto.level)}"`);
  if (!isLocalPath(dto.link)) return bad(`${at} has a link off the web UI`);
  const createdAt = date(dto.created_at, `${at}: created_at`);
  if (!createdAt) return bad(`${at} has no created_at`);
  const sessions = Array.isArray(dto.interrupted_sessions) ? dto.interrupted_sessions : [];
  return {
    id: dto.id,
    projectId: text(dto.project_id),
    projectName: text(dto.project_name),
    taskId: text(dto.task_id),
    taskTitle: text(dto.task_title),
    sessionId: text(dto.session_id),
    source: dto.source as Notification["source"],
    kind: dto.kind as Notification["kind"],
    level: dto.level as Notification["level"],
    title: text(dto.title),
    body: text(dto.body),
    link: dto.link,
    createdAt,
    readAt: date(dto.read_at, `${at}: read_at`),
    resolvedAt: date(dto.resolved_at, `${at}: resolved_at`),
    interruptedSessions: sessions.map((s) => toInterruptedSession(s, at)),
  };
}

function toInterruptedSession(dto: unknown, at: string): InterruptedSession {
  if (!isRecord(dto) || typeof dto.session_id !== "string" || dto.session_id === "") return bad(`${at} lists a session without an id`);
  if (!INTERRUPTED_STATES.includes(dto.state as never)) return bad(`${at} lists session ${dto.session_id} in unknown state "${String(dto.state)}"`);
  if (!isLocalPath(dto.link)) return bad(`${at} lists session ${dto.session_id} with a link off the web UI`);
  return {
    sessionId: dto.session_id,
    projectId: text(dto.project_id),
    taskId: text(dto.task_id),
    taskTitle: text(dto.task_title),
    agentName: text(dto.agent_name),
    link: dto.link,
    state: dto.state as InterruptedSession["state"],
    reason: text(dto.reason),
  };
}

/** GET /api/notifications → {notifications, unread_count, next_before?}. Also the page the island props carry. */
export function toPage(body: unknown): NotificationPage {
  if (!isRecord(body) || !Array.isArray(body.notifications)) return bad("expected {notifications: [...], unread_count}");
  return { notifications: body.notifications.map(toNotification), unreadCount: count(body.unread_count), nextBefore: text(body.next_before) };
}

/** POST /api/notifications/read → {notifications: [changed…], unread_count} */
export function toReadAnswer(body: unknown): Change {
  if (!isRecord(body) || !Array.isArray(body.notifications)) return bad("expected {notifications: [...], unread_count}");
  return { notifications: body.notifications.map(toNotification), unreadCount: count(body.unread_count) };
}

/** One stream message: {changed: [...], unread_count}. */
export function toStreamChange(body: unknown): Change {
  if (!isRecord(body) || !Array.isArray(body.changed)) return bad("expected {changed: [...], unread_count}");
  return { notifications: body.changed.map(toNotification), unreadCount: count(body.unread_count) };
}

function count(value: unknown): number {
  if (typeof value !== "number" || !Number.isInteger(value) || value < 0) return bad("unread_count is not a count");
  return value;
}

function date(value: unknown, what: string): Date | null {
  if (value === undefined || value === null || value === "") return null;
  const parsed = new Date(String(value));
  if (Number.isNaN(parsed.getTime())) return bad(`${what} is not a time`);
  return parsed;
}

function bad(detail: string): never {
  throw new NotificationsSourceError("BAD_RESPONSE", `Unexpected notification: ${detail}.`);
}
