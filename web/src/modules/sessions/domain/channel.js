// The architect channel: a task's architect coordinates the sessions it
// delegates. Delegates message it, it replies, and a status-check loop per
// delegate wakes it to check on them. Mirrors internal/domain/taskchannel.go;
// no I/O, no Alpine, no DOM.

/** The roles a session can have on its task; "" is a peer. */
export const ROLES = Object.freeze(["", "architect", "delegate"]);

/** What a task message is for. */
export const KINDS = Object.freeze(["review_request", "status_report", "question", "reply"]);

/** Where a delegate says it is, on a status_report. */
export const REPORT_STATUSES = Object.freeze(["working", "blocked", "ready_for_review", "done"]);

/** The architect's answer to a delegate's review_request, on a reply. */
export const VERDICTS = Object.freeze(["approved", "changes_requested"]);

/** Where a status-check loop stands. */
export const CHECK_STATES = Object.freeze(["active", "paused", "ended"]);

/** The intervals a person can pick for a status check, in minutes (the server takes 2–240). */
export const INTERVALS = Object.freeze([2, 5, 10, 15, 30, 60, 120, 240]);

/** The interval a loop is created with, and what Resume falls back to. */
export const DEFAULT_INTERVAL = 10;

/**
 * One message between a session and its task's architect. It outlives both.
 *
 * @typedef {object} TaskMessage
 * @property {string} id
 * @property {string} taskId
 * @property {string} fromSessionId
 * @property {string} toSessionId
 * @property {"review_request"|"status_report"|"question"|"reply"} kind
 * @property {string} subject           "" when it has none
 * @property {string} body
 * @property {""|"working"|"blocked"|"ready_for_review"|"done"} status   on a status_report
 * @property {""|"approved"|"changes_requested"} verdict               on a reply to a review_request
 * @property {string} inReplyTo         the message it answers; ""
 * @property {string[]} documentIds     documents of the task it points at
 * @property {string[]} artifactIds     artifacts of the task it points at
 * @property {boolean} delivered        it reached the recipient as a turn; false while the recipient was not running
 * @property {Date | null} deliveredAt
 * @property {Date} createdAt
 *
 * The recurring check on one delegate the architect started directly: each
 * firing wakes the architect with the delegate's state.
 *
 * @typedef {object} StatusCheck
 * @property {string} taskId
 * @property {string} architectSessionId
 * @property {string} delegateSessionId
 * @property {number} everyMinutes   0 while paused
 * @property {Date | null} nextAt
 * @property {Date | null} lastFiredAt
 * @property {number} firedCount
 * @property {"active"|"paused"|"ended"} state
 */

/** Oldest first; the same instant breaks ties by id, so the order is stable. */
const byCreated = (a, b) => a.createdAt.getTime() - b.createdAt.getTime() || (a.id < b.id ? -1 : a.id > b.id ? 1 : 0);

/**
 * The list with message added, or replacing the one with its id (a message
 * that became delivered is announced again). Oldest first.
 * @param {TaskMessage[]} list @param {TaskMessage} message
 */
export function upsertMessage(list, message) {
  return [...list.filter((m) => m.id !== message.id), message].sort(byCreated);
}

/** Merges a page of messages into the list (after a resync). */
export const mergeMessages = (list, more) => more.reduce(upsertMessage, list);

/** The messages from or to a session: a delegate's thread with the architect. */
export const threadOf = (list, sessionId) => list.filter((m) => m.fromSessionId === sessionId || m.toSessionId === sessionId);

/** A delegate's latest status report, or null. */
export function lastReportOf(list, sessionId) {
  for (let i = list.length - 1; i >= 0; i--) {
    if (list[i].fromSessionId === sessionId && list[i].kind === "status_report") return list[i];
  }
  return null;
}

/** The newest message's time, to ask only for what came after it; null for none. */
export const newestAt = (list) => (list.length ? list[list.length - 1].createdAt : null);
