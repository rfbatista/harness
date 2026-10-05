// What the tasks module needs from the outside world. Implemented by
// infrastructure/tasks-gateway.js (over /api) and
// infrastructure/memory-gateway.js; both run testing/gateway-contract.js.

/**
 * @typedef {import("./task.js").Task} Task
 * @typedef {import("./task.js").TaskStatus} TaskStatus
 *
 * @typedef {object} TaskGateway
 * @property {(input: { projectId: string, title: string, description: string, status: TaskStatus }) => Promise<Task>} createTask
 *           Rejects with INVALID_INPUT (no title), INVALID_STATUS or PROJECT_NOT_FOUND.
 * @property {(input: { id: string, title: string, description: string, status: TaskStatus }) => Promise<Task>} updateTask
 *           Replaces title, description and status. Rejects with INVALID_INPUT,
 *           INVALID_STATUS or TICKET_NOT_FOUND.
 * @property {(id: string) => Promise<void>} deleteTask
 *           Rejects with TICKET_NOT_FOUND.
 * @property {(projectId: string, taskId: string) => Promise<{ total: number, live: number }>} countSessions
 *           The task's sessions now, asked right before deleting it.
 * @property {(seed: unknown) => Task} decodeTask
 *           Reads the task the page embeds. Throws BAD_RESPONSE when malformed.
 */

export {};
