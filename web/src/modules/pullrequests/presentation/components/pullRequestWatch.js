// The task page's pull requests: one follower of the project feed that keeps
// the newest PR per workspace in a shared store, for the session rows' and
// the detail header's badges to read.
//
//   <div x-data="pullRequestWatch" data-seed="pull-requests-seed">
//     … <span x-data="pullRequestBadge(row.workspaceId)"> …
//
// The seed carries what the server stores (no host call on render). On a
// reconnect the watch reads each workspace again: GET is local and cheap, and
// it is how a PR opened during the drop shows up.

import { FeedStatus } from "../../../../shared/domain/feed.js";
import { readSeed } from "../../../../shared/presentation/seed.js";
import { newerOf } from "../../domain/pullRequest.js";

/**
 * @typedef {{ byWorkspace: Record<string, import("../../domain/pullRequest.js").PullRequest> }} PullRequestStore
 */

/** @returns {PullRequestStore} */
export const emptyStore = () => ({ byWorkspace: {} });

/** Keeps pr in the store unless it already holds a newer one for the workspace. */
export function keep(store, pr) {
  const next = newerOf(store.byWorkspace[pr.workspaceId] ?? null, pr);
  if (next !== store.byWorkspace[pr.workspaceId]) store.byWorkspace = { ...store.byWorkspace, [pr.workspaceId]: next };
}

/** @param {{ gateway: import("../../domain/ports.js").PullRequestGateway, store: PullRequestStore }} deps */
export const pullRequestWatch = ({ gateway, store }) => () => {
  let unfollow = () => {};
  let workspaceIds = [];

  return {
    projectId: "",

    init() {
      let seed;
      try {
        seed = gateway.decodeWatchSeed(readSeed(this.$el));
      } catch {
        return; // the badges stay empty; the rest of the page is unaffected
      }
      this.projectId = seed.projectId;
      workspaceIds = seed.workspaceIds;
      for (const pr of seed.pullRequests) keep(store, pr);
      unfollow = gateway.follow(
        this.projectId,
        (pr) => keep(store, pr),
        (status) => status === FeedStatus.RESYNCED && this.reread(),
      );
    },

    destroy() {
      unfollow();
    },

    /** Reads every workspace the page shows again, after changes may have been missed. */
    async reread() {
      const ids = [...new Set([...workspaceIds, ...Object.keys(store.byWorkspace)])];
      await Promise.all(
        ids.map(async (id) => {
          try {
            const { pullRequest } = await gateway.get(id);
            if (pullRequest) keep(store, pullRequest);
          } catch {
            // gone (WORKSPACE_NOT_FOUND) or unreachable: its badge keeps what it showed
          }
        }),
      );
    },
  };
};
