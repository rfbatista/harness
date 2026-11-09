// A session's Branches list as the person reads it (Glossary "Branches list";
// Contract: branch requests and the Branches list §1): the session branch and
// every branch the session made or touched, with its state, its commits
// against the branch it started from, and where it was pushed.

/**
 * @typedef {object} SessionBranch
 * @property {string} name
 * @property {boolean} isSessionBranch
 * @property {"session"|"approved"|"denied"|"deleted"|"unapproved"} state
 * @property {{ ref: string, ahead: number, behind: number } | null} base
 * @property {Array<{ remote: string, remoteBranch: string, at: Date, by: "person"|"agent"|"unapproved" }>} pushed
 * @property {string[]} requestIds
 * @property {Date | null} flaggedAt
 */

export const BRANCH_STATES = ["session", "approved", "denied", "deleted", "unapproved"];

const TONES = { approved: "success", denied: "attention", unapproved: "danger" };

const WHO = { person: "you", agent: "the agent", unapproved: "no approval" };

/** "2 branches", "1 branch". */
export const branchesLabel = (n) => `${n} ${n === 1 ? "branch" : "branches"}`;

/** A branch on the list is flagged when it is unapproved, or was pushed around the gate. */
export const isFlagged = (b) => b.state === "unapproved" || b.flaggedAt !== null;

/** How many of the session's branches are flagged. */
export const flaggedCount = (branches) => branches.filter(isFlagged).length;

/** What a template binds for one row. */
export function rowOf(b) {
  const base = b.base;
  return {
    name: b.name,
    state: b.state,
    stateLabel: b.state === "session" ? "session branch" : b.state,
    flagged: isFlagged(b),
    flagAttr: isFlagged(b) ? "" : null,
    tone: TONES[b.state] ?? null,
    hasBase: base !== null,
    baseRef: base?.ref ?? "",
    ahead: base ? String(base.ahead) : "",
    behind: base ? String(base.behind) : "",
    countsLabel: base ? `${base.ahead} ahead, ${base.behind} behind ${base.ref}` : "",
    pushed: b.pushed.map((p) => ({
      key: `${p.remote}/${p.remoteBranch}@${p.at.getTime()}`,
      line: `pushed to ${p.remote}/${p.remoteBranch} by ${WHO[p.by] ?? p.by}`,
      by: p.by,
    })),
    hasPushed: b.pushed.length > 0,
  };
}
