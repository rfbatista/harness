// Notifications: what needs the person, or is worth knowing, across every
// project (Contract: Harness server ↔ Web UI — notifications). Ported from the
// Alpine client's notifications domain, which it does not import. No I/O, no DOM.

export const SOURCES = ["system", "agent"] as const;
export const KINDS = ["review_requested", "session_waiting", "task_review", "session_failed", "commit_check_off", "branch_request", "branch_finding", "service_request", "sessions_interrupted", "agent", "session_memory_killed", "session_memory_kill_failed"] as const;
export const ACTOR_KINDS = ["agent", "system", "person"] as const;
export const OUTCOMES = ["approved", "changes_requested", "denied", "expired", "withdrawn", "answered", "dismissed", "acknowledged", "repaired", "resumed", "ended", "done", "reopened"] as const;
export const LISTED_STATES = ["off", "repaired", "ended"] as const;
export const INTERRUPTED_STATES = ["interrupted", "resumed", "failed", "gone"] as const;
export const LEVELS = ["info", "action"] as const;

export type Source = (typeof SOURCES)[number];
export type Kind = (typeof KINDS)[number];
export type InterruptedState = (typeof INTERRUPTED_STATES)[number];
/** "action": the person has to do something. */
export type Level = (typeof LEVELS)[number];
export type ActorKind = (typeof ACTOR_KINDS)[number];
export type Outcome = (typeof OUTCOMES)[number];
export type ListedState = (typeof LISTED_STATES)[number];

/** Who caused a notification. */
export interface Actor {
  readonly kind: ActorKind;
  /** The agent as its session row shows it; "branch watch", "restart" or "harness" for the system. */
  readonly name: string;
  /** Agent only: the session's mode word; "". */
  readonly mode: string;
  /** Agent only: the session that caused it; "". */
  readonly sessionId: string;
}

/** Who a notification reads as when it carries no actor (revision 5 data). */
export const HARNESS_ACTOR: Actor = { kind: "system", name: "harness", mode: "", sessionId: "" };

/** What a verb acts on; `code` shows it monospace. */
export interface NotificationObject {
  readonly text: string;
  readonly code: boolean;
}

/** One session a commit_check_off item lists. */
export interface ListedSession {
  readonly sessionId: string;
  readonly taskId: string;
  readonly taskTitle: string;
  readonly agentName: string;
  readonly link: string;
  readonly state: ListedState;
}

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
  /** Who caused it; data without one reads as the harness. */
  readonly actor: Actor;
  /** System kinds: the verb phrase in the agent's voice, lower case; "" on kind agent and on data without one. */
  readonly verb: string;
  /** What the verb acts on. */
  readonly object: NotificationObject | null;
  /** "" when the kind has none. */
  readonly repositoryName: string;
  /** How it was settled; "" while open and on rows settled before outcomes were recorded. */
  readonly outcome: Outcome | "";
  readonly listedSessions: readonly ListedSession[];
}

export interface NotificationPage {
  /** Newest first. */
  readonly notifications: readonly Notification[];
  /** Global, whatever the filter. */
  readonly unreadCount: number;
  /** Global open causes (ADR 0096); null from a server that sends none. */
  readonly openCount?: number | null;
  /** The cursor of the next page; "" at the end. */
  readonly nextBefore: string;
}

/** What the stream and a mark-read answer carry: the notifications created, read or resolved, and the global count as of that change. */
export interface Change {
  readonly notifications: readonly Notification[];
  readonly unreadCount: number;
  /** Global open causes as of that change; null from a server that sends none. */
  readonly openCount?: number | null;
}

export interface Filter {
  /** Only the unread ones. */
  readonly unread?: boolean;
  /** Only the open causes (level action, not settled). */
  readonly open?: boolean;
}

/** Unread: neither read nor resolved. */
export const isUnread = (n: Notification): boolean => n.readAt === null && n.resolvedAt === null;

/** Its cause was settled: there is nothing left to do about it. */
export const isSettled = (n: Notification): boolean => n.resolvedAt !== null;

/** An open cause: the person has something to do about it (level action, not settled). */
export const isOpen = (n: Notification): boolean => n.level === "action" && n.resolvedAt === null;

/** Open causes first, each group keeping its order (newest first). */
export const openFirst = (list: readonly Notification[]): Notification[] => [...list.filter(isOpen), ...list.filter((n) => !isOpen(n))];

/** Whether n belongs in a list with this filter. */
export const matches = (filter: Filter, n: Notification): boolean => !(filter.unread && !isUnread(n)) && !(filter.open && !isOpen(n));

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
