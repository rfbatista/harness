// Review requests: the task's architect asks the person to look at something,
// and waits for the answer. Mirrors ReviewRequest in
// internal/domain/taskchannel.go; no I/O, no Alpine, no DOM.

/**
 * @typedef {"pending"|"approved"|"changes_requested"|"withdrawn"} ReviewState
 * @typedef {"approved"|"changes_requested"} Decision
 *
 * @typedef {object} ReviewRequest
 * @property {string} id
 * @property {string} taskId
 * @property {string} projectId
 * @property {string} architectSessionId  the architect that raised it
 * @property {string} aboutSessionId      the delegate whose work it is; "" for the task as a whole
 * @property {string} subject
 * @property {string} body
 * @property {string[]} documentIds
 * @property {{ id: string, href: string }[]} artifacts  the artifacts it points at, with where to open them
 * @property {ReviewState} state
 * @property {string} responseNote        the person's note, once answered
 * @property {Date | null} respondedAt
 * @property {Date} createdAt
 * @property {Date} updatedAt
 *
 * @typedef {{ kind: "review", review: ReviewRequest }} ReviewEvent  a review was raised or changed state
 */

export const STATES = Object.freeze(["pending", "approved", "changes_requested", "withdrawn"]);
export const DECISIONS = Object.freeze(["approved", "changes_requested"]);

export const isPending = (review) => review.state === "pending";

/** Newest first; the same instant breaks ties by id, so the order is stable. */
const byCreated = (a, b) => b.createdAt.getTime() - a.createdAt.getTime() || (a.id < b.id ? 1 : a.id > b.id ? -1 : 0);

/** The list with review added, or replacing the one with its id. Newest first. */
export const upsertReview = (list, review) => [...list.filter((r) => r.id !== review.id), review].sort(byCreated);

/**
 * Why an answer cannot be sent, or "": asking for changes needs a note, since
 * the architect passes it on to whoever does the work.
 */
export function noteProblem(decision, note) {
  if (decision === "changes_requested" && note.trim() === "") return "Say what should change: the architect passes it on.";
  return "";
}
