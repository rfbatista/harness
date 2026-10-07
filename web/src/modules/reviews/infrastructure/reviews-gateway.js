// ReviewGateway over the harness HTTP API (the Web UI contract's
// /api/review_requests and /api/respond_review_request) and the project feed.

import { toResponse, toReviewEvent, toReviews } from "./dto.js";

/**
 * @param {import("../../../shared/infrastructure/api.js").ApiClient} api
 * @param {import("../../../shared/infrastructure/feed.js").Feed} feed
 * @param {string} [base]  the API prefix the browser opens artifacts under
 * @returns {import("../domain/ports.js").ReviewGateway}
 */
export function reviewsGateway(api, feed, base = "/api") {
  return {
    async listForTask(ticketId, signal) {
      return toReviews(await api.get("/review_requests", { ticket_id: ticketId }, signal), base);
    },

    async listPendingForProject(projectId, signal) {
      return toReviews(await api.get("/review_requests", { project_id: projectId, state: "pending" }, signal), base);
    },

    async respond({ reviewId, decision, note }) {
      return toResponse(await api.post("/respond_review_request", { review_id: reviewId, decision, note }), base);
    },

    follow(projectId, onEvent, onStatus) {
      return feed.follow(`/events?project_id=${encodeURIComponent(projectId)}`, {
        onMessage: (dto) => {
          let event;
          try {
            event = toReviewEvent(dto, base);
          } catch {
            return; // a message we cannot read is dropped; the next resync corrects the list
          }
          if (event) onEvent(event);
        },
        onStatus,
      });
    },
  };
}
