// What the reviews module needs from the outside world. Implemented by
// infrastructure/reviews-gateway.js (the Web UI contract's routes and the
// project feed) and infrastructure/memory-gateway.js (tests and web/dev).
// Both run testing/gateway-contract.js.

/**
 * @typedef {import("./review.js").ReviewRequest} ReviewRequest
 * @typedef {import("./review.js").ReviewEvent} ReviewEvent
 * @typedef {import("./review.js").Decision} Decision
 * @typedef {import("../../../shared/domain/feed.js").FeedStatus} FeedStatus
 *
 * @typedef {object} ReviewGateway
 * @property {(ticketId: string, signal?: AbortSignal) => Promise<ReviewRequest[]>} listForTask
 *           Every review request of the task, in every state, newest first.
 * @property {(projectId: string, signal?: AbortSignal) => Promise<ReviewRequest[]>} listPendingForProject
 *           The project's pending review requests, newest first: its inbox.
 * @property {(answer: { reviewId: string, decision: Decision, note: string }) => Promise<{ review: ReviewRequest, delivered: boolean }>} respond
 *           Answers a pending request; the answer goes to the architect.
 *           delivered: it reached the architect as a turn now (false while
 *           the architect is mid-turn or not running: it gets it later).
 *           Rejects with REVIEW_NOT_FOUND, REVIEW_NOT_PENDING (answered or
 *           withdrawn already) or INVALID_INPUT (changes without a note).
 * @property {(projectId: string,
 *             onEvent: (event: ReviewEvent) => void,
 *             onStatus: (status: FeedStatus) => void) => () => void} follow
 *           Every review request of the project raised or changed, until the
 *           returned function is called. Other feed messages are not delivered.
 */

export {};
