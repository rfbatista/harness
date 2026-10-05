// Tasks: the tickets agents work on, in kanban states. Mirrors
// internal/domain/ticket.go. A task can run many sessions; deleting a task
// does not delete them, so a task with sessions is not deleted here.

/**
 * @typedef {"backlog"|"todo"|"in_progress"|"review"|"done"} TaskStatus
 *
 * @typedef {object} Task
 * @property {string} id
 * @property {string} projectId
 * @property {string} title
 * @property {string} description
 * @property {TaskStatus} status
 */

/** The statuses in board order, with how they read. */
export const STATUSES = Object.freeze([
  { value: "backlog", label: "Backlog" },
  { value: "todo", label: "Todo" },
  { value: "in_progress", label: "In progress" },
  { value: "review", label: "Review" },
  { value: "done", label: "Done" },
]);

export const isStatus = (value) => STATUSES.some((s) => s.value === value);

/** A title is required; it is trimmed. */
export const titleProblem = (title) => (title.trim() ? "" : "A task needs a title.");

/**
 * Why a task cannot be deleted, or "" when it can: its sessions would be
 * left under no task.
 * @param {{ total: number, live: number }} sessions
 */
export function deleteBlocker({ total, live }) {
  if (live > 0) return `It has ${live} running ${live === 1 ? "session" : "sessions"}: stop and delete ${live === 1 ? "it" : "them"} first.`;
  if (total > 0) return `It has ${total} ${total === 1 ? "session" : "sessions"}: delete ${total === 1 ? "it" : "them"} first, so ${total === 1 ? "it is" : "they are"} not left under no task.`;
  return "";
}
