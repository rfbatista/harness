// ReviewGateway in memory: the fake for page tests and the data source for
// web/dev pages. It obeys the same contract as the real gateway
// (../testing/gateway-contract.js).

import { Codes, StructuredError } from "../../../shared/domain/errors.js";
import { FeedStatus } from "../../../shared/domain/feed.js";
import { DECISIONS, noteProblem, upsertReview } from "../domain/review.js";

/**
 * @param {{
 *   reviews?: import("../domain/review.js").ReviewRequest[],
 *   now?: () => Date,
 *   delivered?: () => boolean,  whether an answer reaches the architect at once
 * }} [world]
 */
export function memoryReviews({ reviews = [], now = () => new Date(), delivered = () => true } = {}) {
  let list = reviews.reduce(upsertReview, []);
  const followers = new Set();
  let nextId = 1;

  function change(review) {
    list = upsertReview(list, review);
    for (const f of followers) if (f.projectId === review.projectId) f.onEvent({ kind: "review", review });
    return review;
  }

  /** @type {import("../domain/ports.js").ReviewGateway} */
  const gateway = {
    async listForTask(ticketId) {
      return list.filter((r) => r.taskId === ticketId);
    },
    async listPendingForProject(projectId) {
      return list.filter((r) => r.projectId === projectId && r.state === "pending");
    },
    async respond({ reviewId, decision, note }) {
      const review = list.find((r) => r.id === reviewId);
      if (!review) throw new StructuredError(Codes.REVIEW_NOT_FOUND, `review request ${reviewId} not found`, 404);
      if (!DECISIONS.includes(decision)) throw new StructuredError(Codes.INVALID_INPUT, "decision must be approved or changes_requested", 400);
      if (noteProblem(decision, note)) throw new StructuredError(Codes.INVALID_INPUT, "a note is required to request changes", 400);
      if (review.state !== "pending") throw new StructuredError(Codes.REVIEW_NOT_PENDING, `review request ${reviewId} is ${review.state}`, 409);
      const at = now();
      return {
        review: change(Object.freeze({ ...review, state: decision, responseNote: note, respondedAt: at, updatedAt: at })),
        delivered: delivered(),
      };
    },
    follow(projectId, onEvent, onStatus) {
      const follower = { projectId, onEvent };
      followers.add(follower);
      queueMicrotask(() => followers.has(follower) && onStatus(FeedStatus.LIVE));
      return () => followers.delete(follower);
    },
  };

  return {
    gateway,
    /** The architect asks the person for a review, as request_user_review would. */
    request(fields) {
      const at = now();
      return change(
        Object.freeze({
          id: `rv-${nextId++}`,
          taskId: "t1",
          projectId: "p1",
          architectSessionId: "arch",
          aboutSessionId: "",
          subject: "Please review",
          body: "",
          documentIds: [],
          artifacts: [],
          state: "pending",
          responseNote: "",
          respondedAt: null,
          createdAt: at,
          updatedAt: at,
          ...fields,
        }),
      );
    },
    /** The architect withdraws a pending request, as withdraw_user_review would. */
    withdraw(id) {
      const review = list.find((r) => r.id === id);
      return review ? change(Object.freeze({ ...review, state: "withdrawn", updatedAt: now() })) : null;
    },
    find: (id) => list.find((r) => r.id === id) ?? null,
  };
}
