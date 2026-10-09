// Notifications: what needs the person, or is worth knowing, across every
// project. The server records them from what it sees (review requests,
// sessions waiting, tasks in review, failed sessions) and agents post their
// own. Mirrors the Web UI contract's Notification; no I/O, no Alpine, no DOM.

/**
 * @typedef {"system"|"agent"} Source
 * @typedef {"review_requested"|"session_waiting"|"task_review"|"session_failed"|"agent"} Kind
 * @typedef {"info"|"action"} Level  action: the person has to do something
 *
 * @typedef {object} Notification
 * @property {string} id
 * @property {string} projectId
 * @property {string} projectName
 * @property {string} taskId       "" when it is about no task
 * @property {string} taskTitle
 * @property {string} sessionId    the session it came from or is about; ""
 * @property {Source} source
 * @property {Kind} kind
 * @property {Level} level
 * @property {string} title
 * @property {string} body         plain text, never markup
 * @property {string} link         a path on the web UI, starting with "/"
 * @property {Date} createdAt
 * @property {Date | null} readAt      the person read or dismissed it
 * @property {Date | null} resolvedAt  its cause was settled (system kinds only)
 *
 * @typedef {object} Filter
 * @property {string} [projectId]  "" or absent: every project
 * @property {boolean} [unread]    only the unread ones
 *
 * @typedef {object} Page
 * @property {Notification[]} notifications  newest first
 * @property {number} unreadCount             global, whatever the filter
 * @property {string} nextBefore              the cursor of the next page; "" at the end
 *
 * @typedef {object} Change  what the stream and a mark-read answer carry
 * @property {Notification[]} notifications  the ones created, read or resolved
 * @property {number} unreadCount            global, as of that change
 */

export const SOURCES = Object.freeze(["system", "agent"]);
export const KINDS = Object.freeze(["review_requested", "session_waiting", "task_review", "session_failed", "agent"]);
export const LEVELS = Object.freeze(["info", "action"]);

/** Unread: neither read nor resolved. */
export const isUnread = (n) => n.readAt === null && n.resolvedAt === null;

/** Its cause was settled: there is nothing left to do about it. */
export const isSettled = (n) => n.resolvedAt !== null;

/** Whether n belongs in a list with this filter. */
export function matches(filter, n) {
  if (filter.projectId && n.projectId !== filter.projectId) return false;
  if (filter.unread && !isUnread(n)) return false;
  return true;
}

/** Newest first; the same instant breaks ties by id, so the order is stable. */
const byCreated = (a, b) => b.createdAt.getTime() - a.createdAt.getTime() || (a.id < b.id ? 1 : a.id > b.id ? -1 : 0);

/** The list with each of changed added, or replacing the one with its id. Newest first. */
export function upsert(list, changed) {
  const ids = new Set(changed.map((n) => n.id));
  return [...list.filter((n) => !ids.has(n.id)), ...changed].sort(byCreated);
}

/**
 * What a list with `filter` shows after `changed`: the ones it has are
 * replaced where they stand, even when they no longer match (a row read in
 * an Unread list stays until the list is read again, rather than jumping
 * away under the pointer); new ones join only when they match.
 */
export function merge(list, changed, filter) {
  const have = new Set(list.map((n) => n.id));
  return upsert(list, changed.filter((n) => have.has(n.id) || matches(filter, n)));
}

/** n as read at `at`; one already read or resolved is returned as is. */
export const markedRead = (n, at) => (isUnread(n) ? Object.freeze({ ...n, readAt: at }) : n);
