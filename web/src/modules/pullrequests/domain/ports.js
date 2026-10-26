// What the pull requests module needs from the outside world. Implemented by
// infrastructure/pullrequests-gateway.js (the Web UI contract's routes and the
// project feed) and infrastructure/memory-gateway.js (tests and web/dev).
// Both run testing/gateway-contract.js.

/**
 * @typedef {import("./pullRequest.js").PullRequest} PullRequest
 * @typedef {import("./pullRequest.js").PullRequestView} PullRequestView
 * @typedef {import("./pullRequest.js").PullRequestBases} PullRequestBases
 * @typedef {import("../../../shared/domain/feed.js").FeedStatus} FeedStatus
 *
 * @typedef {object} PanelSeed  the history page's: one workspace
 * @property {string} projectId
 * @property {string} workspaceId
 * @property {PullRequestView | null} view  null when the server painted none (read it with get)
 *
 * @typedef {object} WatchSeed  the task page's: its sessions' workspaces
 * @property {string} projectId
 * @property {string[]} workspaceIds
 * @property {PullRequest[]} pullRequests  the newest stored per workspace that has one
 *
 * @typedef {object} PullRequestGateway
 * @property {(workspaceId: string, signal?: AbortSignal) => Promise<PullRequestView>} get
 *           The draft and the newest PR stored for the workspace. Never waits
 *           on the host: what the host says arrives later on the feed.
 *           Rejects with WORKSPACE_NOT_FOUND once the workspace is gone.
 * @property {(workspaceId: string, signal?: AbortSignal) => Promise<PullRequestBases>} bases
 *           Origin's branches as last fetched, from local git: never fetches.
 *           Rejects with WORKSPACE_NOT_FOUND.
 * @property {(workspaceId: string) => Promise<PullRequestBases>} refreshBases
 *           Fetches origin with the stored credentials, then answers. A failure
 *           leaves the previous list readable through bases: rejects with
 *           PR_NO_REMOTE, GIT_CREDENTIAL_MISSING, GIT_CREDENTIAL_REJECTED or
 *           PR_FETCH_FAILED.
 * @property {(req: { workspaceId: string, title: string, body: string, baseBranch: string, draft: boolean }) => Promise<PullRequest>} create
 *           Pushes the branch and opens the PR. Rejects with INVALID_INPUT,
 *           WORKSPACE_NOT_FOUND or a PR_* code; PR_ALREADY_EXISTS carries the
 *           open one as error.details.pullRequest.
 * @property {(projectId: string,
 *             onPullRequest: (pr: PullRequest) => void,
 *             onStatus: (status: FeedStatus) => void) => () => void} follow
 *           Every PR of the project opened, adopted or changed, until the
 *           returned function is called. Other feed messages are not delivered.
 * @property {(raw: unknown) => PanelSeed} decodePanelSeed
 * @property {(raw: unknown) => WatchSeed} decodeWatchSeed
 */

export {};
