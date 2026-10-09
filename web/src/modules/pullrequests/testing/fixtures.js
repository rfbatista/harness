// Pull requests and drafts for tests and web/dev pages.

export const P0 = new Date("2026-10-07T21:00:00Z");

/** @returns {import("../domain/pullRequest.js").PullRequest} */
export function makePullRequest(overrides = {}) {
  return Object.freeze({
    workspaceId: "ws1",
    repositoryId: "r1",
    number: 42,
    url: "https://github.com/acme/harness/pull/42",
    title: "Create pull request",
    headBranch: "agent/create-pull-request-9d728ce8",
    baseBranch: "main",
    draft: false,
    state: "open",
    createdAt: P0,
    ...overrides,
  });
}

/** @returns {import("../domain/pullRequest.js").PullRequestDraft} */
export function makeDraft(overrides = {}) {
  return Object.freeze({
    workspaceId: "ws1",
    repositoryId: "r1",
    headBranch: "agent/create-pull-request-9d728ce8",
    baseBranch: "main",
    title: "Create pull request",
    body: "I need a way to create a pull request from the ui\n\n- feat(web): pull request panel",
    ahead: 3,
    uncommittedChanges: 0,
    remote: { host: "github.com", owner: "acme", repo: "harness" },
    blockers: [],
    ...overrides,
  });
}

/** A memory workspace whose draft is makeDraft(overrides). */
export function makeWorkspace(overrides = {}) {
  const d = makeDraft(overrides);
  return { id: d.workspaceId, projectId: "p1", ...d, ...overrides };
}

/** The wire format of a pull request. */
export function pullRequestDTO(pr) {
  return {
    workspace_id: pr.workspaceId,
    repository_id: pr.repositoryId,
    number: pr.number,
    url: pr.url,
    title: pr.title,
    head_branch: pr.headBranch,
    base_branch: pr.baseBranch,
    draft: pr.draft,
    state: pr.state,
    created_at: pr.createdAt.toISOString(),
  };
}

/** The wire format of a draft. */
export function draftDTO(d) {
  return {
    workspace_id: d.workspaceId,
    repository_id: d.repositoryId,
    head_branch: d.headBranch,
    base_branch: d.baseBranch,
    title: d.title,
    body: d.body,
    ahead: d.ahead,
    uncommitted_changes: d.uncommittedChanges,
    remote: d.remote,
    blockers: [...d.blockers],
  };
}
