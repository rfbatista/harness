// PullRequestGateway over the harness HTTP API (the Web UI contract's
// /api/pull_request and /api/create_pull_request) and the project feed.

import { createBody, toBases, toCreated, toCreateError, toPanelSeed, toPullRequestEvent, toView, toWatchSeed } from "./dto.js";

/**
 * @param {import("../../../shared/infrastructure/api.js").ApiClient} api
 * @param {import("../../../shared/infrastructure/feed.js").Feed} feed
 * @returns {import("../domain/ports.js").PullRequestGateway}
 */
export function pullRequestsGateway(api, feed) {
  return {
    async get(workspaceId, signal) {
      return toView(await api.get("/pull_request", { workspace_id: workspaceId }, signal));
    },

    async bases(workspaceId, signal) {
      return toBases(await api.get("/pull_request_bases", { workspace_id: workspaceId }, signal));
    },

    async refreshBases(workspaceId) {
      return toBases(await api.post("/refresh_pull_request_bases", { workspace_id: workspaceId }));
    },

    async create(req) {
      try {
        return toCreated(await api.post("/create_pull_request", createBody(req)));
      } catch (err) {
        throw toCreateError(err);
      }
    },

    follow(projectId, onPullRequest, onStatus) {
      return feed.follow(`/events?project_id=${encodeURIComponent(projectId)}`, {
        onMessage: (dto) => {
          let pr;
          try {
            pr = toPullRequestEvent(dto);
          } catch {
            return; // a message we cannot read is dropped; the next resync corrects it
          }
          if (pr) onPullRequest(pr);
        },
        onStatus,
      });
    },

    decodePanelSeed: toPanelSeed,
    decodeWatchSeed: toWatchSeed,
  };
}
