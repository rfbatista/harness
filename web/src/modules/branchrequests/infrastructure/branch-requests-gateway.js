// The branch requests over the harness HTTP API and the project feed.

import { toAnswer, toRequestEvent, toRequests } from "./dto.js";

/**
 * @param {import("../../../shared/infrastructure/api.js").ApiClient} api
 * @param {import("../../../shared/infrastructure/feed.js").Feed} feed
 */
export function branchRequestsGateway(api, feed) {
  return {
    /** The task's pending branch requests, newest first. */
    async listPendingForTask(ticketId, signal) {
      return toRequests(await api.get("/branch_requests", { task_id: ticketId, state: "pending" }, signal));
    },

    /** Answers a pending request. Rejects with BRANCH_REQUEST_ANSWERED when it no longer is. */
    async answer({ id, decision, note }) {
      return toAnswer(await api.post(`/branch_requests/${encodeURIComponent(id)}/answer`, { decision, note }));
    },

    follow(projectId, onEvent, onStatus) {
      return feed.follow(`/events?project_id=${encodeURIComponent(projectId)}`, {
        onMessage: (dto) => {
          let event;
          try {
            event = toRequestEvent(dto);
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
