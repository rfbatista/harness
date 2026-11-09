// What the sync module needs from the outside world. Implemented by
// infrastructure/sync-gateway.js (the contract's routes and the project feed)
// and infrastructure/memory-gateway.js (tests and web/dev). Both run
// testing/gateway-contract.js.

/**
 * @typedef {import("./gitState.js").GitState} GitState
 * @typedef {import("../../../shared/domain/feed.js").FeedStatus} FeedStatus
 *
 * @typedef {object} SyncGateway
 * @property {(workspaceId: string, signal?: AbortSignal) => Promise<GitState>} get
 *           GET /api/worktree_git_state. Never fetches or waits on a network.
 *           Rejects with WORKSPACE_NOT_FOUND, INVALID_INPUT or UNAVAILABLE.
 * @property {(taskId: string, signal?: AbortSignal) => Promise<GitState[]>} listForTask
 *           GET /api/list_worktree_git_states, in the task page's session order.
 * @property {(sessionId: string, signal?: AbortSignal) => Promise<{ sessionId: string, branches: import("./branches.js").SessionBranch[] }>} [branchesOf]
 *           GET /api/session_branches: the session's Branches list, from stored state.
 * @property {(taskId: string, signal?: AbortSignal) => Promise<Array<{ sessionId: string, branches: import("./branches.js").SessionBranch[] }>>} [branchesForTask]
 *           GET /api/list_session_branches.
 * @property {(repositoryId: string) => Promise<{ fetchedAt: Date }>} fetchRemote
 *           POST /api/fetch_remote: git fetch origin --prune with the stored
 *           credential. Rejects with REPOSITORY_NOT_FOUND, NO_REMOTE,
 *           GIT_CREDENTIAL_MISSING / _REJECTED / _STORE_LOCKED or FETCH_FAILED.
 * @property {(req: { workspaceId: string, expectedHead: string }) => Promise<GitState>} push
 *           POST /api/push_branch: never forced. Rejects with WORKSPACE_NOT_FOUND,
 *           WORKSPACE_MISSING, NO_REMOTE, a GIT_CREDENTIAL_* code, PUSH_REJECTED
 *           or PUSH_STALE (both carry error.details.state, a GitState) or PUSH_FAILED.
 * @property {(projectId: string,
 *             onState: (state: GitState) => void,
 *             onStatus: (status: FeedStatus) => void,
 *             onBranches?: (list: { sessionId: string, branches: import("./branches.js").SessionBranch[] }) => void) => () => void} follow
 *           Every `git_state` of the project, until the returned function is
 *           called. Other feed messages are not delivered.
 * @property {(raw: unknown) => { projectId: string, states: GitState[] }} decodeSeed
 */

export {};
