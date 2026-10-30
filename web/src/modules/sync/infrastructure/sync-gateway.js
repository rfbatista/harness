// SyncGateway over the harness HTTP API (Contract: worktree git state, fetch
// and push) and the project feed's `git_state` key.

import { toFetched, toGitStateEvent, toPushError, toSeed, toStateAnswer, toStates } from "./dto.js";

/**
 * @param {import("../../../shared/infrastructure/api.js").ApiClient} api
 * @param {import("../../../shared/infrastructure/feed.js").Feed} feed
 * @returns {import("../domain/ports.js").SyncGateway}
 */
export function syncGateway(api, feed) {
  return {
    async get(workspaceId, signal) {
      return toStateAnswer(await api.get("/worktree_git_state", { workspace_id: workspaceId }, signal));
    },

    async listForTask(taskId, signal) {
      return toStates(await api.get("/list_worktree_git_states", { task_id: taskId }, signal));
    },

    async fetchRemote(repositoryId) {
      return toFetched(await api.post("/fetch_remote", { repository_id: repositoryId }));
    },

    async push({ workspaceId, expectedHead }) {
      try {
        return toStateAnswer(await api.post("/push_branch", { workspace_id: workspaceId, expected_head: expectedHead }));
      } catch (err) {
        throw toPushError(err);
      }
    },

    follow(projectId, onState, onStatus) {
      return feed.follow(`/events?project_id=${encodeURIComponent(projectId)}`, {
        onMessage: (dto) => {
          let state;
          try {
            state = toGitStateEvent(dto);
          } catch {
            return; // a message we cannot read is dropped; the next resync corrects it
          }
          if (state) onState(state);
        },
        onStatus,
      });
    },

    decodeSeed: toSeed,
  };
}
