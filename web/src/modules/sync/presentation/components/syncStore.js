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
import { canPush, fresher } from "../../domain/gitState.js";
import { refusalOf } from "../view.js";

/**
 * @typedef {object} SyncStore
 * @property {Record<string, import("../../domain/gitState.js").GitState>} byWorkspace
 * @property {Record<string, true>} fetching     by repository id
 * @property {Record<string, true>} pushing      by workspace id
 * @property {Record<string, ReturnType<typeof refusalOf>>} fetchFailure  by repository id
 * @property {Record<string, ReturnType<typeof refusalOf>>} pushFailure   by workspace id
 */

/** @returns {SyncStore} */
export const emptyStore = () => ({ byWorkspace: {}, fetching: {}, pushing: {}, fetchFailure: {}, pushFailure: {} });

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

  const workspacesOf = (repositoryId) => Object.values(store.byWorkspace).filter((s) => s.repositoryId === repositoryId);

  /** Reads a workspace again, after changes may have been missed. Gone (WORKSPACE_NOT_FOUND) or unreachable keeps what is shown. */
  async function reread(workspaceId) {
    try {
      keep(await gateway.get(workspaceId));
    } catch {
      // its chip keeps what it showed
    }
  }

  return {
    keep,

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
}
