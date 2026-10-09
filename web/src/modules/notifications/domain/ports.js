// What the notifications module needs from the outside world. Implemented by
// infrastructure/notifications-gateway.js (the Web UI contract's
// /api/notifications routes and the global notifications stream) and
// infrastructure/memory-gateway.js (tests and web/dev). Both run
// testing/gateway-contract.js.

/**
 * @typedef {import("./notification.js").Notification} Notification
 * @typedef {import("./notification.js").Page} Page
 * @typedef {import("./notification.js").Change} Change
 * @typedef {import("../../../shared/domain/feed.js").FeedStatus} FeedStatus
 *
 * @typedef {object} ListQuery
 * @property {string} [projectId]  absent: every project
 * @property {boolean} [unread]
 * @property {string} [before]     a page's nextBefore
 * @property {number} [limit]      1..200; the server's default is 50
 *
 * @typedef {{ ids: string[] } | { all: true, projectId?: string }} ReadRequest
 *
 * @typedef {object} NotificationGateway
 * @property {(query: ListQuery, signal?: AbortSignal) => Promise<Page>} list
 *           Newest first, across every project unless projectId narrows it.
 *           unreadCount is the global count whatever the filter. Rejects
 *           with INVALID_INPUT for a bad limit or cursor.
 * @property {(request: ReadRequest) => Promise<Change>} markRead
 *           Marks the ids, or every unread one (all), optionally narrowed to
 *           a project. Answers with the ones it changed; ids already read
 *           are not an error. Rejects with NOTIFICATION_NOT_FOUND for an
 *           unknown id, INVALID_INPUT for neither ids nor all.
 * @property {(onChange: (change: Change) => void,
 *             onStatus: (status: FeedStatus) => void) => () => void} follow
 *           Every notification created, read or resolved, in every project,
 *           with the global count as of that change, in order, until the
 *           returned function is called. The person's own reads echo here.
 */

export {};
