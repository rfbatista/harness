// SyncGateway over the harness HTTP API (Contract: worktree git state, fetch
// and push) and the project feed's `git_state` key.

import { toBranchesEvent, toFetched, toGitStateEvent, toPushError, toPushPreview, toRemotes, toSeed, toSessionBranchesAnswer, toStateAnswer, toStates, toTaskBranches } from "./dto.js";

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

    async branchesOf(sessionId, signal) {
      return toSessionBranchesAnswer(sessionId, await api.get("/session_branches", { session_id: sessionId }, signal));
    },

    async branchesForTask(taskId, signal) {
      return toTaskBranches(await api.get("/list_session_branches", { task_id: taskId }, signal));
    },

    async fetchRemote(repositoryId) {
      return toFetched(await api.post("/fetch_remote", { repository_id: repositoryId }));
    },

    async remotes(repositoryId, signal) {
      return toRemotes(await api.get("/repository_remotes", { repository_id: repositoryId }, signal));
    },

    async pushPreview({ workspaceId, branch, remote, remoteBranch }, signal) {
      const query = { workspace_id: workspaceId, branch, remote };
      if (remoteBranch) query.remote_branch = remoteBranch;
      return toPushPreview(await api.get("/push_preview", query, signal));
    },

    async push({ workspaceId, expectedHead, branch, remote, remoteBranch }) {
      const body = { workspace_id: workspaceId, expected_head: expectedHead };
      if (branch) body.branch = branch;
      if (remote) body.remote = remote;
      if (remoteBranch) body.remote_branch = remoteBranch;
      try {
        return toStateAnswer(await api.post("/push_branch", body));
      } catch (err) {
        throw toPushError(err);
      }
    },

    follow(projectId, onState, onStatus, onBranches = () => {}) {
      return feed.follow(`/events?project_id=${encodeURIComponent(projectId)}`, {
        onMessage: (dto) => {
          try {
            const state = toGitStateEvent(dto);
            if (state) onState(state);
            const branches = toBranchesEvent(dto);
            if (branches) onBranches(branches);
          } catch {
            // a message we cannot read is dropped; the next resync corrects it
          }
        },
        onStatus,
      });
    },

    decodeSeed: toSeed,
  };
}
