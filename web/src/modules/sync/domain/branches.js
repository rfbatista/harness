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

/** A branch the person may push: the session's own or an approved one. */
export const isPushable = (b) => b.state === "session" || b.state === "approved";

/** The Push confirmation's heading for a preview, naming the remote, the remote branch and the commit count. */
export function pushQuestion(branch, push) {
  const target = `${push.remote}/${push.preview?.remoteBranch ?? branch}`;
  if (!push.preview) return `Push ${branch} to ${target}?`;
  const n = push.preview.commits;
  const commits = `${n} ${n === 1 ? "commit" : "commits"}`;
  return push.preview.commitsAgainst === "remote"
    ? `Push ${commits} of ${branch} to ${target}?`
    : `Push ${branch} to ${target}? ${commits} ahead of its base; ${target} is not known here.`;
}

/**
 * What a template binds for one row. `ctx` is where Push stands for the
 * workspace the list belongs to: its remotes (undefined until read) and the
 * confirmation open on one of its rows, if any.
 * @param {SessionBranch} b
 * @param {{ remotes?: Array<{ name: string }>, push?: { branch: string, remote: string, preview: object | null, failure: object | null } | null, pushing?: boolean }} [ctx]
 */
export function rowOf(b, ctx = {}) {
  const base = b.base;
  const remotes = ctx.remotes ?? [];
  const push = ctx.push && ctx.push.branch === b.name ? ctx.push : null;
  return {
    offersPush: isPushable(b) && remotes.length > 0 && !push,
    confirming: !!push,
    question: push ? pushQuestion(b.name, push) : "",
    remoteOptions: remotes.map((r) => ({ name: r.name, selected: push?.remote === r.name })),
    pushRefusal: push?.failure ?? null,
    pushRefusalBanner: push?.failure?.tone === "banner",
    pushRefusalStrip: push?.failure?.tone === "danger",
    noRemote: isPushable(b) && ctx.remotes !== undefined && remotes.length === 0,
    pushing: !!push && ctx.pushing === true,
    previewLoading: !!push && !push.preview && !push.failure,
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
