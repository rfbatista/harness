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
      unfollow = actions.watch(this.projectId);
    },

    destroy() {
      unfollow();
    },
  };
};
