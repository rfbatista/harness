// The page's one follower of the project feed's `git_state`, filling the
// shared store the chips, panels and the popover read:
//
//   <div x-data="syncWatch" data-seed="sessions-seed"> … <span x-data="syncChip(row.workspaceId)">
//
// The seed (`git_states` in the page's sessions-seed) is what the server painted, so chips render
// without a second round trip. On a reconnect every workspace held is read
// again: GET is served from the code watcher's snapshot, and it is how a push
// or a worktree change during the drop shows up.

import { readSeed } from "../../../../shared/presentation/seed.js";

/** @param {{ gateway: import("../../domain/ports.js").SyncGateway, actions: ReturnType<typeof import("./syncStore.js").syncActions> }} deps */
export const syncWatch = ({ gateway, actions }) => () => {
  let unfollow = () => {};

  return {
    projectId: "",

    init() {
      let seed;
      try {
        seed = gateway.decodeSeed(readSeed(this.$el));
      } catch {
        return; // the chips stay empty; the rest of the page is unaffected
      }
      this.projectId = seed.projectId;
      for (const state of seed.states) actions.keep(state);
      for (const list of seed.sessionBranches) actions.keepBranches(list);
      unfollow = actions.watch(this.projectId);
    },

    destroy() {
      unfollow();
    },
  };
};

/**
 * The git states that a task page's lazy section brings after the page, in
 * a seed of their own (ADR 0064): they fill the same store the watch follows.
 *
 *   <div x-data="syncStates" data-seed="git-states-seed" hidden></div>
 *
 * @param {{ gateway: import("../../domain/ports.js").SyncGateway, actions: ReturnType<typeof import("./syncStore.js").syncActions>, loading?: { end: () => Promise<void> } }} deps
 */
export const syncStates = ({ gateway, actions, loading }) => () => ({
  async init() {
    let states = [];
    try {
      states = gateway.decodeSeed(readSeed(this.$el)).states;
    } catch {
      // the chips stay empty; the feed's git_state fills them as it comes
    }
    // The chips take their place once the skeleton chips are gone, so they never stack on them.
    if (loading) await loading.end();
    for (const state of states) actions.keep(state);
  },
});

/**
 * The placeholder the task page answers with in place of the git states
 * (hx-trigger="load"): while it is on the page the session rows draw skeleton
 * chips, by the shared rule (ADR 0064). syncStates ends the load when the
 * section replaces it; a failed request ends it with the rows as they were.
 *
 *   <div id="git-states" hx-get="…" hx-trigger="load" hx-swap="outerHTML" hidden
 *        x-data="syncStatesLoading" x-on:htmx:response-error="failed" x-on:htmx:send-error="failed" x-on:htmx:timeout="failed"></div>
 *
 * @param {{ loading: { begin: () => void, fail: () => void, stop: () => void, readonly running: boolean }, timers?: { setTimeout: typeof setTimeout } }} deps
 */
export const syncStatesLoading = ({ loading, timers = globalThis }) => () => ({
  init() {
    loading.begin();
  },
  failed() {
    loading.fail();
  },
  destroy() {
    // The section's own element replaces this one and ends the load as it starts;
    // a page that left before it came ends it here, so no skeleton stays.
    timers.setTimeout(() => {
      if (loading.running) loading.stop();
    }, 0);
  },
});
