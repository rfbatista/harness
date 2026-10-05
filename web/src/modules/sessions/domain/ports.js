// What the sessions module needs from the outside world.
//
// Implemented by infrastructure/sessions-gateway.js (over /api) and
// infrastructure/memory-gateway.js (in memory, for tests and web/dev).
// Both run testing/gateway-contract.js.

/**
 * @typedef {import("./session.js").Session} Session
 * @typedef {import("./session.js").SessionChange} SessionChange
 * @typedef {import("../../../shared/domain/feed.js").FeedStatus} FeedStatus
 *
 * @typedef {object} SessionGateway
 * @property {(seed: unknown) => { projectId: string, sessions: Session[] }} decodeSeed
 *           Reads the page seed the server embedded (same JSON shape as the
 *           API). Throws BAD_RESPONSE on a malformed seed.
 * @property {(projectId: string, signal?: AbortSignal) => Promise<Session[]>} list
 *           A project's sessions. Rejects with PROJECT_NOT_FOUND.
 * @property {(sessionId: string, text: string) => Promise<void>} send
 *           Sends the developer's next message. Rejects with SESSION_NOT_FOUND
 *           or SESSION_NOT_RUNNING.
 * @property {(sessionId: string) => Promise<void>} stop
 *           Stops the session. Rejects with SESSION_NOT_FOUND.
 * @property {(projectId: string,
 *             onChange: (change: SessionChange) => void,
 *             onStatus: (status: FeedStatus) => void) => () => void} follow
 *           Follows the project's changes until the returned function is called.
 */

export {};
