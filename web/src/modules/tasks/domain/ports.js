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
 * @property {(documentId: string, scope: "task" | "project") => Promise<import("./documents.js").DocumentVersion & { scope: "task" | "project" }>} setDocumentScope
 *           Moves a document between task and project scope; the task keeps
 *           it. The same scope again changes nothing. Rejects with
 *           DOCUMENT_NOT_FOUND or INVALID_INPUT (unknown scope).
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
 * @typedef {RailChange | import("./board.js").TaskChange | import("./board.js").StatusChangeEvent} ProjectChange
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

/**
 * A task's resources and comments: what the person added for context, read
 * and written over /api and followed on the project feed. Implemented by
 * infrastructure/resources-gateway.js (over /api) and
 * infrastructure/memory-resources.js; both run testing/resource-contract.js.
 *
 * @typedef {import("./resources.js").Resource} Resource
 * @typedef {import("./comments.js").Comment} Comment
 * @typedef {{ kind: "resource", resource: Resource } | { kind: "resource-deleted", id: string, ticketId: string }
 *         | { kind: "comment", comment: Comment } | { kind: "comment-deleted", id: string, ticketId: string }
 *         | { kind: "task-deleted", id: string }} ResourceChange
 *
 * @typedef {object} ResourceGateway
 * @property {(seed: unknown) => { projectId: string, ticketId: string, resources: Resource[], comments: Comment[] }} decodeSeed
 *           Reads the page's embedded seed. Throws BAD_RESPONSE when malformed.
 * @property {(ticketId: string) => Promise<Resource[]>} listResources
 *           Oldest first. Rejects with TICKET_NOT_FOUND.
 * @property {(input: { ticketId: string, file: Blob & { name: string }, title?: string,
 *             onProgress?: (loaded: number, total: number) => void, signal?: AbortSignal }) => Promise<Resource>} upload
 *           Rejects with RESOURCE_TOO_LARGE, INVALID_INPUT (empty file, bad
 *           title), TICKET_NOT_FOUND or NETWORK; a cancel rejects with an AbortError.
 * @property {(input: { ticketId: string, url: string, title?: string }) => Promise<Resource>} addLink
 *           Rejects with INVALID_INPUT (not http(s), too long) or TICKET_NOT_FOUND.
 * @property {(resourceId: string, title: string) => Promise<Resource>} retitle
 *           Rejects with INVALID_INPUT (blank, too long) or RESOURCE_NOT_FOUND.
 * @property {(resourceId: string) => Promise<void>} removeResource
 *           Rejects with RESOURCE_NOT_FOUND, also when it is already gone.
 * @property {(ticketId: string) => Promise<Comment[]>} listComments
 *           Oldest first. Rejects with TICKET_NOT_FOUND.
 * @property {(ticketId: string, body: string) => Promise<Comment>} addComment
 *           Rejects with INVALID_INPUT (empty, over 10,000) or TICKET_NOT_FOUND.
 * @property {(commentId: string, body: string) => Promise<Comment>} editComment
 *           An edit that changes nothing keeps updated_at. Rejects with
 *           INVALID_INPUT or COMMENT_NOT_FOUND.
 * @property {(commentId: string) => Promise<void>} removeComment
 *           Rejects with COMMENT_NOT_FOUND, also when it is already gone.
 * @property {(projectId: string, onChange: (change: ResourceChange) => void,
 *             onStatus: (status: import("../../../shared/domain/feed.js").FeedStatus) => void) => () => void} follow
 *           Resource and comment changes of every task of the project, and
 *           tasks deleted; returns the unfollow. Other messages, and ones it
 *           cannot read, are not delivered.
 */

export {};

/**
 * The rail's remembered folding: which status groups the person collapsed.
 * Implemented by infrastructure/rail-prefs.js (the rail_collapsed cookie, which
 * the server reads to paint the first load folded) and
 * infrastructure/memory-rail-prefs.js; both run testing/rail-prefs-contract.js.
 *
 * @typedef {object} RailPrefs
 * @property {() => string[]} collapsed  status keys; [] when none or unreadable
 * @property {(keys: string[]) => void} setCollapsed  remembers them; [] forgets
 */
