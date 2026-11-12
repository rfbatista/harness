// The sync module's shared state and what changes it: the GitState held per
// workspace, Fetch and Push with their in-flight guard, and the feed's echoes.
// One store per page, read by every chip, panel and the popover.
//
// The guard: a fetch is in flight per repository (every workspace of it), a
// push per workspace. The feed echoes the person's own action, possibly before
// its POST answers. While an action is in flight, states that arrive for the
// workspaces it covers are held, not shown; when the answer lands, the newest
// state by the server's read time wins, so the chip never flickers back to an
// older count and ends on one consistent state.

import { Codes, codeOf } from "../../../../shared/domain/errors.js";
import { FeedStatus } from "../../../../shared/domain/feed.js";
import { canPush, fresher } from "../../domain/gitState.js";
import { refusalOf } from "../view.js";

/**
 * @typedef {object} SyncStore
 * @property {Record<string, import("../../domain/gitState.js").GitState>} byWorkspace
 * @property {Record<string, true>} fetching     by repository id
 * @property {Record<string, true>} pushing      by workspace id
 * @property {Record<string, ReturnType<typeof refusalOf>>} fetchFailure  by repository id
 * @property {Record<string, ReturnType<typeof refusalOf>>} pushFailure   by workspace id
 * @property {Record<string, import("../../domain/branches.js").SessionBranch[]>} branches  each session's Branches list, by session id
 * @property {Record<string, Array<{ name: string, url: string }>>} remotes  each repository's remotes, origin first, by repository id
 * @property {{ workspaceId: string, repositoryId: string, branch: string, remote: string, preview: { head: string, commits: number, commitsAgainst: string, remoteBranch: string } | null, failure: ReturnType<typeof refusalOf> | null } | null} branchPush  the Push confirmation open on a Branches row; one at a time
 * @property {boolean} statesLoading  the task page's lazy git section is loading and its skeleton chips show
 * @property {string} statesIn        "skeleton-in" once the chips arrived after a skeleton
 */

/** @returns {SyncStore} */
export const emptyStore = () => ({ byWorkspace: {}, fetching: {}, pushing: {}, fetchFailure: {}, pushFailure: {}, branches: {}, remotes: {}, branchPush: null, statesLoading: false, statesIn: "" });

const without = (record, key) => {
  const { [key]: _, ...rest } = record;
  return rest;
};

/** @param {{ gateway: import("../../domain/ports.js").SyncGateway, store: SyncStore }} deps */
export function syncActions({ gateway, store }) {
  /** Newest state that arrived for a workspace while an action on it was in flight. */
  const held = new Map();

  const inFlight = (state) => !!store.pushing[state.workspaceId] || !!store.fetching[state.repositoryId];

  function apply(state) {
    const next = fresher(store.byWorkspace[state.workspaceId], state);
    if (next !== store.byWorkspace[state.workspaceId]) store.byWorkspace = { ...store.byWorkspace, [state.workspaceId]: next };
  }

  /** A state from the feed, a read or a seed. */
  function keep(state) {
    if (inFlight(state)) held.set(state.workspaceId, fresher(held.get(state.workspaceId), state));
    else apply(state);
  }

  /** Shows what was held for the workspace, if it is newer than what is on screen. */
  function release(workspaceId) {
    const state = held.get(workspaceId);
    held.delete(workspaceId);
    if (state) apply(state);
  }

  /** A session's Branches list from the feed, a seed or a read. */
  function keepBranches(list) {
    store.branches = { ...store.branches, [list.sessionId]: list.branches };
  }

  /** Sessions whose list was asked for already, so a panel reads each once. */
  const askedBranches = new Set();

  /** Repositories whose remotes were asked for already. */
  const askedRemotes = new Set();

  /** Reads what the open confirmation would push: the head and the commit count for the remote picked. */
  async function loadPreview() {
    const ask = store.branchPush;
    if (!ask || !gateway.pushPreview) return;
    try {
      const preview = await gateway.pushPreview({ workspaceId: ask.workspaceId, branch: ask.branch, remote: ask.remote });
      if (store.branchPush === ask) store.branchPush = { ...ask, preview };
    } catch (err) {
      if (store.branchPush === ask) store.branchPush = { ...ask, failure: refusalOf(err) };
    }
  }

  const workspacesOf = (repositoryId) => Object.values(store.byWorkspace).filter((s) => s.repositoryId === repositoryId);

  /** Reads a workspace again, after changes may have been missed. Gone (WORKSPACE_NOT_FOUND) or unreachable keeps what is shown. */
  async function reread(workspaceId) {
    try {
      keep(await gateway.get(workspaceId));
    } catch {
      // its chip keeps what it showed
    }
  }

  /** Workspaces already asked for, so a page of many chips reads each once. */
  const asked = new Set();

  /** The project feeds followed, by project id, with how many hold each. */
  const feeds = new Map();

  const api = {
    keep,
    keepBranches,

    /** Reads the session's Branches list when the page holds none for it (a panel the page's seed did not cover). */
    async ensureBranches(sessionId) {
      if (!sessionId || store.branches[sessionId] || askedBranches.has(sessionId) || !gateway.branchesOf) return;
      askedBranches.add(sessionId);
      try {
        keepBranches(await gateway.branchesOf(sessionId));
      } catch {
        // the panel shows no list; the feed fills it when one changes
      }
    },

    /** Reads the repository's remotes once, for the remote picker of its Branches rows. */
    async ensureRemotes(repositoryId) {
      if (!repositoryId || store.remotes[repositoryId] || askedRemotes.has(repositoryId) || !gateway.remotes) return;
      askedRemotes.add(repositoryId);
      try {
        const remotes = await gateway.remotes(repositoryId);
        store.remotes = { ...store.remotes, [repositoryId]: remotes };
      } catch {
        askedRemotes.delete(repositoryId); // asked again when the panel next opens
      }
    },

    /** Opens the Push confirmation on a row, origin picked when the repository has it, and reads what it would push. */
    async askBranchPush({ workspaceId, repositoryId, branch }) {
      if (!workspaceId || store.pushing[workspaceId]) return;
      const remotes = store.remotes[repositoryId] ?? [];
      const remote = (remotes.find((r) => r.name === "origin") ?? remotes[0])?.name;
      if (!remote) return;
      store.branchPush = { workspaceId, repositoryId, branch, remote, preview: null, failure: null };
      await loadPreview();
    },

    /** The person picked another remote: the count is read again for it. */
    async pickBranchRemote(remote) {
      const ask = store.branchPush;
      if (!ask || ask.remote === remote || store.pushing[ask.workspaceId]) return;
      store.branchPush = { ...ask, remote, preview: null, failure: null };
      await loadPreview();
    },

    cancelBranchPush() {
      if (store.branchPush && !store.pushing[store.branchPush.workspaceId]) store.branchPush = null;
    },

    /**
     * Pushes the row's branch to the remote picked, at the head the preview
     * named. Returns whether it pushed; a refusal stays on the confirmation.
     */
    async confirmBranchPush() {
      const ask = store.branchPush;
      if (!ask?.preview || store.pushing[ask.workspaceId]) return false;
      const { workspaceId } = ask;
      store.pushing = { ...store.pushing, [workspaceId]: true };
      store.branchPush = { ...ask, failure: null };
      let ok = false;
      try {
        const after = await gateway.push({ workspaceId, expectedHead: ask.preview.head, branch: ask.branch, remote: ask.remote, remoteBranch: ask.preview.remoteBranch });
        held.set(workspaceId, fresher(held.get(workspaceId), after));
        ok = true;
        store.branchPush = null;
        if (gateway.branchesOf && after.sessionId) {
          try {
            keepBranches(await gateway.branchesOf(after.sessionId));
          } catch {
            // the feed brings the row's new push line
          }
        }
      } catch (err) {
        const reread = err?.details?.state;
        if (reread && (codeOf(err) === Codes.PUSH_REJECTED || codeOf(err) === Codes.PUSH_STALE)) held.set(workspaceId, fresher(held.get(workspaceId), reread));
        if (store.branchPush) store.branchPush = { ...store.branchPush, failure: refusalOf(err) };
        // The branch moved: the confirmation shows what it is now, and the next Push goes at that head.
        if (codeOf(err) === Codes.PUSH_STALE && store.branchPush) store.branchPush = { ...store.branchPush, preview: null };
      } finally {
        store.pushing = without(store.pushing, workspaceId);
        release(workspaceId);
      }
      if (!ok && store.branchPush && !store.branchPush.preview) await loadPreview();
      return ok;
    },

    /** Reads every list the page holds again, after the feed dropped. */
    async rereadBranches() {
      if (!gateway.branchesOf) return;
      await Promise.all(Object.keys(store.branches).map(async (id) => {
        try {
          keepBranches(await gateway.branchesOf(id));
        } catch {
          // keeps what is shown
        }
      }));
    },


    /**
     * Follows the project feed's git_state into the store, once per project
     * however many hold it (the page's watch, each Code tab's chip); the
     * returned function lets go, and the last one stops the feed. A reconnect
     * reads every workspace held again.
     */
    watch(projectId) {
      if (!projectId) return () => {};
      let feed = feeds.get(projectId);
      if (!feed) {
        const stop = gateway.follow(
          projectId,
          (state) => keep(state),
          (status) => {
            if (status !== FeedStatus.RESYNCED) return;
            api.rereadAll();
            api.rereadBranches();
          },
          (list) => keepBranches(list),
        );
        feed = { holders: 0, stop };
        feeds.set(projectId, feed);
      }
      feed.holders += 1;
      let released = false;
      return () => {
        if (released) return;
        released = true;
        feed.holders -= 1;
        if (feed.holders === 0) {
          feed.stop();
          feeds.delete(projectId);
        }
      };
    },

    /**
     * Reads a workspace's state when the page holds none for it: a chip the
     * page's seed did not cover (the Code tab and the History page ask for
     * their own worktree). Asked once; unreachable or gone leaves it without a chip.
     */
    async ensure(workspaceId) {
      if (!workspaceId || store.byWorkspace[workspaceId] || asked.has(workspaceId)) return;
      asked.add(workspaceId);
      await reread(workspaceId);
    },

    /** What is running for the workspace: "pushing", "fetching" or "". */
    busy(state) {
      if (!state) return "";
      return store.pushing[state.workspaceId] ? "pushing" : store.fetching[state.repositoryId] ? "fetching" : "";
    },

    /** Reads every workspace the page holds. */
    async rereadAll() {
      await Promise.all(Object.keys(store.byWorkspace).map(reread));
    },

    /**
     * Fetches the repository's origin, then reads its workspaces again (the
     * feed tells them too; a stopped session's page would not otherwise know).
     * Returns whether it fetched; a refusal is in store.fetchFailure.
     */
    async fetchRepository(repositoryId) {
      if (!repositoryId || store.fetching[repositoryId]) return false;
      store.fetching = { ...store.fetching, [repositoryId]: true };
      store.fetchFailure = without(store.fetchFailure, repositoryId);
          const covered = workspacesOf(repositoryId).map((s) => s.workspaceId);
      let ok = false;
      try {
        await gateway.fetchRemote(repositoryId);
        ok = true;
        await Promise.all([...new Set([...covered, ...workspacesOf(repositoryId).map((s) => s.workspaceId)])].map(reread));
      } catch (err) {
        store.fetchFailure = { ...store.fetchFailure, [repositoryId]: refusalOf(err) };
      } finally {
        store.fetching = without(store.fetching, repositoryId);
        for (const [id, state] of [...held]) if (state.repositoryId === repositoryId && !store.pushing[id]) release(id);
      }
      return ok;
    },

    /**
     * Pushes the workspace's branch at the head the person saw. Returns whether
     * it pushed; a refusal is in store.pushFailure, and the state the server
     * re-read with PUSH_REJECTED or PUSH_STALE replaces what is shown.
     */
    async push(workspaceId) {
      const state = store.byWorkspace[workspaceId];
      if (!state || store.pushing[workspaceId] || !canPush(state)) return false;
      store.pushing = { ...store.pushing, [workspaceId]: true };
      store.pushFailure = without(store.pushFailure, workspaceId);
      let ok = false;
      try {
        const after = await gateway.push({ workspaceId, expectedHead: state.head });
        held.set(workspaceId, fresher(held.get(workspaceId), after));
        ok = true;
      } catch (err) {
        const reread = err?.details?.state;
        if (reread && (codeOf(err) === Codes.PUSH_REJECTED || codeOf(err) === Codes.PUSH_STALE)) held.set(workspaceId, fresher(held.get(workspaceId), reread));
        store.pushFailure = { ...store.pushFailure, [workspaceId]: refusalOf(err) };
      } finally {
        store.pushing = without(store.pushing, workspaceId);
        release(workspaceId);
      }
      return ok;
    },
  };
  return api;
}
