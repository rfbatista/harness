// The architect channel: a task's architect coordinates the sessions it
// delegates. Delegates message it, it replies, and a status-check loop per
// delegate wakes it to check on them. Mirrors internal/domain/taskchannel.go;
// no I/O, no Alpine, no DOM.

/** The roles a session can have on its task; "" is a peer. */
export const ROLES = Object.freeze(["", "architect", "delegate"]);

/** Where a status-check loop stands. */
export const CHECK_STATES = Object.freeze(["active", "paused", "ended"]);

/**
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

export {};
