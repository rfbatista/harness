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
 * @property {(input: { id: string, title?: string, description?: string, status?: TaskStatus }) => Promise<Task>} updateTask
 *           Sends only the keys given; an omitted one keeps its value on the
 *           server. Rejects with INVALID_INPUT (title given but blank, or an
 *           unknown status) or TICKET_NOT_FOUND.
 * @property {(id: string, status: TaskStatus) => Promise<Task>} moveTask
 *           A status-only update: what the board sends when a card is moved.
 * @property {(id: string) => Promise<void>} deleteTask
 *           Rejects with TICKET_NOT_FOUND.
 * @property {(projectId: string, taskId: string) => Promise<{ total: number, live: number }>} countSessions
 *           The task's sessions now, asked right before deleting it.
 * @property {(taskId: string) => Promise<import("./documents.js").DocumentVersion[]>} listDocumentVersions
 *           Which documents are linked to the task now, and their versions.
 * @property {(seed: unknown) => Task} decodeTask
 *           Reads the task the page embeds. Throws BAD_RESPONSE when malformed.
 */

/**
 * The project's live view for the rail and the board: its tasks and its
 * sessions, seeded by the page and followed over the project feed.
 * Implemented by infrastructure/rail-gateway.js (over /api) and
 * infrastructure/memory-rail.js; both run testing/rail-contract.js.
 *
 * @typedef {import("./activity.js").RailSession} RailSession
 * @typedef {{ kind: "upsert", session: RailSession } | { kind: "deleted", id: string }} RailChange
 * @typedef {RailChange | import("./board.js").TaskChange} ProjectChange
 *
 * @typedef {object} RailGateway
 * @property {(seed: unknown) => { projectId: string, sessions: RailSession[], tasks: Task[] }} decodeSeed
 *           Reads the rail's embedded seed. Throws BAD_RESPONSE when malformed.
 * @property {(projectId: string) => Promise<RailSession[]>} listSessions
 *           The project's sessions now: after the feed resyncs.
 * @property {(projectId: string) => Promise<Task[]>} listTasks
 *           The project's tasks now: after the feed resyncs.
 * @property {(projectId: string, onChange: (change: ProjectChange) => void,
 *             onStatus: (status: import("../../../shared/domain/feed.js").FeedStatus) => void) => () => void} follow
 *           Every change to the project's sessions and tasks; returns the
 *           unfollow. Messages of other kinds, or ones it cannot read, are
 *           not delivered.
 */

export {};
