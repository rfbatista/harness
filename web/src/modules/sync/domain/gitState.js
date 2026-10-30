// GitState: where a session worktree's branch stands against its base and its
// upstream (Contract: worktree git state, fetch and push). Plain values and
// rules; nothing here knows the wire, the feed or Alpine.

/**
 * @typedef {"in_sync" | "ahead" | "behind" | "diverged" | "none" | "gone" | "unknown"} UpstreamState
 *
 * @typedef {object} Upstream
 * @property {UpstreamState} state
 * @property {string | null} ref     "origin/agent/foo-1234"; null for none
 * @property {string | null} remote  "origin"; null for none
 * @property {number | null} ahead   null for none, gone and unknown
 * @property {number | null} behind
 *
 * @typedef {object} GitState
 * @property {string} workspaceId
 * @property {string} repositoryId
 * @property {string} sessionId
 * @property {string} branch         "" when detached
 * @property {string} head           full hash; "" before the first commit
 * @property {boolean} gone          the worktree is no longer on disk
 * @property {{ ref: string, gone: boolean, ahead: number, behind: number }} base  gone when ref no longer resolves
 * @property {Upstream} upstream
 * @property {Date | null} fetchedAt when the repository's origin was last fetched
 * @property {Date | null} pushedAt  when this server last pushed the branch
 * @property {{ tracked: number, untracked: number, staged: number }} uncommitted
 * @property {Date} readAt           when the server read this state
 */

export const UPSTREAM_STATES = Object.freeze(["in_sync", "ahead", "behind", "diverged", "none", "gone", "unknown"]);

/** A fetch older than this makes the counts, and the age line, amber. */
export const OLD_AFTER_MS = 60 * 60 * 1000;

/**
 * The upstream state git's counts and the fetch time mean, as the contract's table says.
 * Used by the in-memory gateway; the server's answer is always taken as it comes.
 * @param {{ ref: string | null, ahead: number, behind: number, fetchedAt: Date | null, pushedAt?: Date | null, exists?: boolean }} facts
 * @returns {UpstreamState}
 */
export function upstreamStateOf({ ref, ahead, behind, fetchedAt, pushedAt = null, exists = true }) {
  if (!ref) return "none";
  if (!exists) return "gone";
  if (!fetchedAt && !pushedAt) return "unknown";
  if (ahead > 0 && behind > 0) return "diverged";
  if (ahead > 0) return "ahead";
  if (behind > 0) return "behind";
  return "in_sync";
}

/**
 * Which of two states of one workspace to hold: the one the server read last.
 * A tie goes to the incoming one (the same read, or a later one in the same instant).
 * @param {GitState | null | undefined} held
 * @param {GitState} incoming
 */
export function fresher(held, incoming) {
  if (!held) return incoming;
  return incoming.readAt.getTime() >= held.readAt.getTime() ? incoming : held;
}

/** The commits a push would publish: what is ahead of the upstream, or of the base when there is none. null when unknown. */
export function commitsToPush(state) {
  const { upstream } = state;
  if (upstream.state === "ahead") return upstream.ahead ?? null;
  if (upstream.state === "none") return state.base.gone ? null : state.base.ahead;
  return null;
}

/**
 * Push is offered only when the branch is ahead of its upstream, or has no
 * upstream and has commits; never for a detached HEAD, a gone worktree or an
 * unreadable upstream (behind, diverged, gone, unknown, in sync).
 * @param {GitState} state
 */
export function canPush(state) {
  if (state.gone || state.branch === "" || state.head === "") return false;
  if (state.upstream.state === "ahead") return true;
  if (state.upstream.state !== "none") return false;
  return state.base.gone ? true : state.base.ahead > 0;
}

/** The remote branch a push writes: the upstream's ref, or origin/<branch> when there is none. */
export function pushTarget(state) {
  return state.upstream.ref ?? `${state.upstream.remote ?? "origin"}/${state.branch}`;
}

/** Whether the counts are over an hour behind (never fetched counts as old). */
export function isOld(fetchedAt, now) {
  return !fetchedAt || now.getTime() - fetchedAt.getTime() > OLD_AFTER_MS;
}
