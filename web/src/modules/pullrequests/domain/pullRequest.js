// Pull requests: a session's branch proposed for merging into its base on the
// repository's host. Mirrors PullRequest and PullRequestDraft in
// internal/domain (the Web UI contract's shapes); no I/O, no Alpine, no DOM.

/**
 * @typedef {"open"|"closed"|"merged"} PullRequestState
 * @typedef {"open"|"draft"|"closed"|"merged"} DisplayState  draft is an open PR marked draft
 *
 * @typedef {object} PullRequest
 * @property {string} workspaceId
 * @property {string} repositoryId
 * @property {number} number
 * @property {string} url          the host's page for it
 * @property {string} title
 * @property {string} headBranch
 * @property {string} baseBranch
 * @property {boolean} draft
 * @property {PullRequestState} state
 * @property {Date} createdAt
 *
 * @typedef {{ host: string, owner: string, repo: string }} Remote
 *
 * @typedef {object} PullRequestDraft  what opening one would do now
 * @property {string} workspaceId
 * @property {string} repositoryId
 * @property {string} headBranch
 * @property {string} baseBranch
 * @property {string} title               suggested
 * @property {string} body                suggested
 * @property {number} ahead               commits on head that base lacks
 * @property {number} uncommittedChanges  files changed but not committed: left out of the PR
 * @property {Remote | null} remote       null when there is no origin
 * @property {string[]} blockers          codes that would refuse a create, as far as the server knows locally
 *
 * @typedef {{ draft: PullRequestDraft, pullRequest: PullRequest | null }} PullRequestView
 *
 * @typedef {object} PullRequestBases  the branches a PR can be based on: origin's, as last fetched
 * @property {string} workspaceId
 * @property {readonly string[]} branches  by name, without the origin/ prefix
 * @property {string} default              the branch the workspace was cut from; first in the picker
 * @property {Date | null} fetchedAt       when origin was last fetched; null when unknown
 *
 * @typedef {object} PullRequestForm
 * @property {string} title
 * @property {string} body
 * @property {string} baseBranch
 * @property {boolean} draft
 */

export const STATES = Object.freeze(["open", "closed", "merged"]);

export const isOpen = (pr) => pr?.state === "open";

/** @returns {DisplayState} */
export const displayState = (pr) => (pr.state === "open" && pr.draft ? "draft" : pr.state);

/**
 * Whether a create may be offered: no open PR (a merged or closed one may be
 * followed by another) and nothing the server knows of in the way.
 */
export const canCreate = (draft, pr) => !!draft && !isOpen(pr) && draft.blockers.length === 0;

/**
 * The picker's options: the default first, then origin's branches by name. The
 * default stays listed when origin was not fetched yet and does not show it.
 * @param {PullRequestBases | null} bases
 * @param {string} fallback  the draft's base, when there is no list yet
 */
export function baseOptions(bases, fallback = "") {
  const first = bases?.default || fallback;
  const rest = (bases?.branches ?? []).filter((b) => b !== first);
  return [...(first ? [first] : []), ...rest];
}

/** The form as the draft suggests it. */
export function formFromDraft(draft) {
  return { title: draft.title, body: draft.body, baseBranch: draft.baseBranch, draft: false };
}

/**
 * Why the form cannot be sent, per field ("" when it can). An empty base is
 * fine: the server takes the draft's.
 */
export function formProblems(form) {
  return { title: form.title.trim() === "" ? "A pull request needs a title." : "", baseBranch: "" };
}

export const hasProblems = (problems) => Object.values(problems).some((p) => p !== "");

/**
 * Which of two PRs of a workspace to show: the newer one (a workspace may
 * have a merged PR and a later open one); the same PR replaces itself.
 */
export function newerOf(current, next) {
  if (!current || current.number === next.number) return next;
  return next.createdAt.getTime() >= current.createdAt.getTime() ? next : current;
}
