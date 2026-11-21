// Notifications: what needs the person, or is worth knowing, across every
// project (Contract: Harness server ↔ Web UI — notifications). Ported from the
// Alpine client's notifications domain, which it does not import. No I/O, no DOM.

export const SOURCES = ["system", "agent"] as const;
export const KINDS = ["review_requested", "session_waiting", "task_review", "session_failed", "commit_check_off", "branch_request", "branch_finding", "sessions_interrupted", "agent"] as const;
export const INTERRUPTED_STATES = ["interrupted", "resumed", "failed", "gone"] as const;
export const LEVELS = ["info", "action"] as const;

export type Source = (typeof SOURCES)[number];
export type Kind = (typeof KINDS)[number];
export type InterruptedState = (typeof INTERRUPTED_STATES)[number];
/** "action": the person has to do something. */
export type Level = (typeof LEVELS)[number];

/** One session a restart interrupted, as of the last change. */
export interface InterruptedSession {
  readonly sessionId: string;
  readonly projectId: string;
  readonly taskId: string;
  readonly taskTitle: string;
  readonly agentName: string;
  /** The session's page, starting with "/". */
  readonly link: string;
  readonly state: InterruptedState;
  /** Why the last Resume all failed; "" unless state is "failed". */
  readonly reason: string;
}

export interface Notification {
  readonly id: string;
  readonly projectId: string;
  readonly projectName: string;
  /** "" when it is about no task. */
  readonly taskId: string;
  readonly taskTitle: string;
  readonly sessionId: string;
  readonly source: Source;
  readonly kind: Kind;
  readonly level: Level;
  readonly title: string;
  /** Plain text, never markup. */
  readonly body: string;
  /** A path on the web UI, starting with "/". */
  readonly link: string;
  readonly createdAt: Date;
  /** The person read or dismissed it. */
  readonly readAt: Date | null;
  /** Its cause was settled (system kinds only). */
  readonly resolvedAt: Date | null;
  readonly interruptedSessions: readonly InterruptedSession[];
}

export interface NotificationPage {
  /** Newest first. */
  readonly notifications: readonly Notification[];
  /** Global, whatever the filter. */
  readonly unreadCount: number;
  /** The cursor of the next page; "" at the end. */
  readonly nextBefore: string;
}

/** What the stream and a mark-read answer carry: the notifications created, read or resolved, and the global count as of that change. */
export interface Change {
  readonly notifications: readonly Notification[];
  readonly unreadCount: number;
}

export interface Filter {
  /** Only the unread ones. */
  readonly unread?: boolean;
}

/** Unread: neither read nor resolved. */
export const isUnread = (n: Notification): boolean => n.readAt === null && n.resolvedAt === null;

/** Its cause was settled: there is nothing left to do about it. */
export const isSettled = (n: Notification): boolean => n.resolvedAt !== null;

/** Whether n belongs in a list with this filter. */
export const matches = (filter: Filter, n: Notification): boolean => !(filter.unread && !isUnread(n));

/** n as read at `at`; one already read or resolved is returned as is. */
export const markedRead = (n: Notification, at: Date): Notification => (isUnread(n) ? { ...n, readAt: at } : n);

/** Newest first; the same instant breaks ties by id, so the order is stable. */
const byCreated = (a: Notification, b: Notification): number => b.createdAt.getTime() - a.createdAt.getTime() || (a.id < b.id ? 1 : a.id > b.id ? -1 : 0);

/** The list with each of `changed` added, or replacing the one with its id. Newest first. */
export function upsert(list: readonly Notification[], changed: readonly Notification[]): Notification[] {
  const ids = new Set(changed.map((n) => n.id));
  return [...list.filter((n) => !ids.has(n.id)), ...changed].sort(byCreated);
}

/**
 * What a list with `filter` shows after `changed`: the ones it has are
 * replaced where they stand, even when they no longer match (a row read in an
 * Unread list stays until the list is read again, rather than jumping away
 * under the pointer); new ones join only when they match.
 */
export function merge(list: readonly Notification[], changed: readonly Notification[], filter: Filter): Notification[] {
  const have = new Set(list.map((n) => n.id));
  return upsert(
    list,
    changed.filter((n) => have.has(n.id) || matches(filter, n)),
  );
}
