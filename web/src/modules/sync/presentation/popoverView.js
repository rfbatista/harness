// The Git popover of the task page: one row per session worktree of the task,
// the foot's fetch age and one Fetch per repository. Plain functions over the
// sessions the page holds and the states the store holds; the component binds
// what they return. Copy follows the UX study (the popover of section 3.4).

import { branchesLabel, flaggedCount, rowOf as branchRowOf } from "../domain/branches.js";
import { fetchAge, panelOf, uncommittedLine } from "./view.js";

/** What a template needs to draw a refusal: the credential banner or the danger strip. */
const refusalFlags = (refusal) => ({ refusalBanner: refusal?.tone === "banner", refusalStrip: refusal?.tone === "danger" });

const agentOf = (agentId, agentNames) => (agentId ? agentNames[agentId] || agentId : "plain claude");

/** The session's Code tab. */
export const codeHref = (projectId, ticketId, sessionId) => `/projects/${encodeURIComponent(projectId)}/tasks/${encodeURIComponent(ticketId)}/sessions/${encodeURIComponent(sessionId)}/code`;

/**
 * What a Branches section binds: its rows, the count and whether any is flagged.
 * @param {import("../domain/branches.js").SessionBranch[] | undefined} list
 * @param {Parameters<typeof branchRowOf>[1]} [ctx]  the remotes and the open Push confirmation of the list's workspace
 */
export function branchesOf(list, ctx = {}) {
  const branches = list ?? [];
  const flagged = flaggedCount(branches);
  return {
    rows: branches.map((b) => branchRowOf(b, ctx)),
    count: branches.length,
    label: branchesLabel(branches.length),
    flagged: flagged > 0,
    flaggedLabel: flagged === 1 ? "1 unapproved" : `${flagged} unapproved`,
    shown: branches.length > 0,
  };
}

/**
 * @param {{
 *   sessions: Array<{ id: string, ticketId: string, agentId: string, repositoryId: string, workspaceId: string, branch: string }>,
 *   states: Record<string, import("../domain/gitState.js").GitState>,
 *   agentNames?: Record<string, string>,
 *   repositoryNames?: Record<string, string>,
 *   projectId: string,
 *   ticketId: string,
 *   now?: Date,
 *   fetching?: Record<string, true>,   by repository id
 *   pushing?: Record<string, true>,    by workspace id
 *   confirming?: string,               the workspace whose push question is asked
 *   pushFailure?: Record<string, object>,   the last refused push by workspace id (refusalOf's shape)
 *   fetchFailure?: Record<string, object>,  the last refused fetch by repository id
 *   branches?: Record<string, import("../domain/branches.js").SessionBranch[]>,  Branches lists by session id
 *   remotes?: Record<string, Array<{ name: string }>>,  each repository's remotes, by repository id
 *   branchPush?: object | null,       the Push confirmation open on a Branches row (the store's)
 * }} input
 */
export function popoverOf({ sessions, states, agentNames = {}, repositoryNames = {}, projectId, ticketId, now = new Date(), fetching = {}, pushing = {}, confirming = "", pushFailure = {}, fetchFailure = {}, branches = {}, remotes = {}, branchPush = null }) {
  const worktrees = sessions.filter((s) => s.workspaceId && (!ticketId || s.ticketId === ticketId));

  const rows = worktrees.map((s) => {
    const state = states[s.workspaceId] ?? null;
    const base = {
      workspaceId: s.workspaceId,
      sessionId: s.id,
      repositoryId: s.repositoryId,
      agent: agentOf(s.agentId, agentNames),
      branch: state?.branch || s.branch,
      codeHref: codeHref(projectId, ticketId || s.ticketId, s.id),
      hasState: state !== null,
      branches: branchesOf(branches[s.id], { remotes: remotes[s.repositoryId], push: branchPush?.workspaceId === s.workspaceId ? branchPush : null }),
      gone: false,
      offersPush: false,
      showsPush: false,
      confirming: false,
    };
    if (!state) return base;
    const busy = pushing[s.workspaceId] ? "pushing" : fetching[state.repositoryId] ? "fetching" : "";
    const panel = panelOf(state, { now, busy });
    const up = panel.upstream.chip;
    const bs = panel.base.chip;
    return {
      ...base,
      gone: state.gone,
      baseState: bs.state,
      baseAhead: bs.ahead,
      baseBehind: bs.behind,
      baseHasCounts: bs.hasCounts,
      baseWord: bs.word,
      upstreamState: up.state,
      upstreamRemote: up.upstream,
      upstreamAhead: up.ahead,
      upstreamBehind: up.behind,
      upstreamHasCounts: up.hasCounts,
      upstreamWord: up.word,
      upstreamLoading: up.loading,
      upstreamOld: up.old,
      upstreamLoadingAttr: up.loading ? "" : null,
      upstreamOldAttr: up.old ? "" : null,
      upstreamTitle: up.describe,
      uncommitted: state.gone ? "worktree gone" : uncommittedLine(state.uncommitted),
      codeHref: state.gone ? `${base.codeHref}/changes?scope=branch` : base.codeHref,
      offersPush: !state.gone && panel.offersPush,
      confirming: !state.gone && panel.offersPush && confirming === s.workspaceId,
      showsPush: !state.gone && panel.offersPush && confirming !== s.workspaceId,
      question: panel.question,
      pushLabel: panel.pushLabel,
      busy: panel.busy,
      refusal: pushFailure[s.workspaceId] ?? null,
      ...refusalFlags(pushFailure[s.workspaceId]),
    };
  });

  const repositoryIds = [...new Set(worktrees.map((s) => s.repositoryId).filter(Boolean))];
  const fetches = repositoryIds.map((id) => {
    const name = repositoryNames[id] ?? "";
    const busy = !!fetching[id];
    return { repositoryId: id, name, busy, refusal: fetchFailure[id] ?? null, ...refusalFlags(fetchFailure[id]), label: busy ? "Fetching…" : name ? `↻ Fetch ${name}` : "↻ Fetch" };
  });

  // The oldest fetch among the worktrees shown: one never fetched makes it "not yet".
  const fetched = rows.filter((r) => r.hasState).map((r) => states[r.workspaceId].fetchedAt);
  const oldest = fetched.length > 0 && fetched.every(Boolean) ? new Date(Math.min(...fetched.map((t) => t.getTime()))) : null;
  const age = fetchAge(oldest, now);

  return {
    shown: worktrees.length > 0,
    rows,
    fetches,
    busy: fetches.some((f) => f.busy),
    fresh: { line: age.never ? "Not fetched yet" : `Fetched ${age.ago}`, old: age.old },
  };
}
