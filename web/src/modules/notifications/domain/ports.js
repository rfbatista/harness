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
 * @property {(seed: unknown) => Page} fromSeed
 *           Decodes the page the server embedded for the first paint (the
 *           GET's JSON). Throws BAD_RESPONSE when it cannot be read.
 * @property {(onChange: (change: Change) => void,
 *             onStatus: (status: FeedStatus) => void) => () => void} follow
 *           Every notification created, read or resolved, in every project,
 *           with the global count as of that change, in order, until the
 *           returned function is called. The person's own reads echo here.
 *
 * @typedef {"default"|"granted"|"denied"|"unsupported"} AlertPermission
 *
 * @typedef {object} DesktopAlerts  the browser's own notifications, outside the page
 * @property {() => AlertPermission} permission
 * @property {() => Promise<AlertPermission>} request  asks the person; call it only from their click
 * @property {(alert: { title: string, body: string, tag: string, onClick: () => void }) => void} show
 * @property {() => boolean} hidden  whether the page is out of sight (another tab, minimised)
 */

export {};
