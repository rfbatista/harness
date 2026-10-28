// Git states for tests and web/dev pages.

export const T0 = new Date("2026-10-28T12:00:00Z");
export const HEAD = "ab6f51b9d3c0e1f2a3b4c5d6e7f8091a2b3c4d5e";

const UP = (state, ahead, behind, ref = "origin/agent/sync-1234") =>
  Object.freeze({ state, ref: ref ? ref : null, remote: ref ? "origin" : null, ahead, behind });

/** The upstream facts of each state of the study's mockup. */
export const UPSTREAMS = Object.freeze({
  in_sync: UP("in_sync", 0, 0),
  ahead: UP("ahead", 3, 0),
  behind: UP("behind", 0, 2),
  diverged: UP("diverged", 3, 2),
  none: Object.freeze({ state: "none", ref: null, remote: null, ahead: null, behind: null }),
  gone: Object.freeze({ state: "gone", ref: "origin/agent/sync-1234", remote: "origin", ahead: null, behind: null }),
  unknown: Object.freeze({ state: "unknown", ref: "origin/agent/sync-1234", remote: "origin", ahead: null, behind: null }),
});

/**
 * A GitState; `upstream` may be a state name (UPSTREAMS) or an upstream object.
 * @returns {import("../domain/gitState.js").GitState}
 */
export function makeState({ upstream = "ahead", ...overrides } = {}) {
  const up = typeof upstream === "string" ? UPSTREAMS[upstream] : upstream;
  return Object.freeze({
    workspaceId: "ws1",
    repositoryId: "r1",
    sessionId: "s1",
    branch: "agent/sync-1234",
    head: HEAD,
    gone: false,
    base: Object.freeze({ ref: "main", ahead: 3, behind: 8 }),
    upstream: up,
    fetchedAt: new Date(T0.getTime() - 12 * 60000),
    uncommitted: Object.freeze({ tracked: 3, untracked: 2, staged: 1 }),
    readAt: T0,
    ...overrides,
  });
}
