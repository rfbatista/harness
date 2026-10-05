// The rail, live. The server renders the project's tasks with each one's
// dot and count; tasksRail follows the project's session feed and keeps a
// shared store of per-task activity, and each link (tasksRailLink) binds its
// dot and count to it — so a session started anywhere (by an agent, the TUI,
// another tab) shows on the rail as it starts.
//
//   <nav x-data="tasksRail" data-seed="rail-seed">
//     <a x-data="tasksRailLink" data-task-id="t1"> … <span x-show="hasDot" …>

import { FeedStatus } from "../../../../shared/domain/feed.js";
import { readSeed } from "../../../../shared/presentation/seed.js";
import { activityByTask, applyRailChange, linkState } from "../../domain/activity.js";

/**
 * The store the rail writes and its links read: Alpine.store("tasksRail").
 * @typedef {{ byTask: Record<string, import("../../domain/activity.js").Activity> }} RailStore
 */

/** @param {{ gateway: import("../../domain/ports.js").RailGateway, store: RailStore }} deps */
export const rail = ({ gateway, store }) => () => {
  let unfollow = () => {};
  let sessions = [];

  return {
    projectId: "",

    init() {
      try {
        const seed = gateway.decodeSeed(readSeed(this.$el));
        this.projectId = seed.projectId;
        sessions = seed.sessions;
      } catch {
        return; // the server's rendering stays as it is
      }
      this.recount();
      unfollow = gateway.follow(
        this.projectId,
        (change) => {
          sessions = applyRailChange(sessions, change);
          this.recount();
        },
        (status) => {
          if (status === FeedStatus.RESYNCED) this.resync();
        },
      );
    },

    /** Changes were missed while the stream was down: read the sessions again. */
    async resync() {
      try {
        sessions = await gateway.listSessions(this.projectId);
        this.recount();
      } catch {
        // keep what it shows; the next resync tries again
      }
    },

    recount() {
      store.byTask = activityByTask(sessions);
    },

    destroy() {
      unfollow();
    },
  };
};

const QUIET = Object.freeze({ live: 0, attention: false });

/** @param {{ store: RailStore }} deps */
export const railLink = ({ store }) => () => ({
  taskId: "",

  init() {
    this.taskId = this.$el.dataset.taskId ?? "";
  },

  get activity() {
    return store.byTask[this.taskId] ?? QUIET;
  },
  get live() {
    return this.activity.live;
  },
  get hasCount() {
    return this.activity.live > 0;
  },
  get hasDot() {
    return linkState(this.activity).state !== "";
  },
  get dotState() {
    return linkState(this.activity).state;
  },
  get dotWord() {
    return linkState(this.activity).word;
  },
});
