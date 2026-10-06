// The rail, live. The server renders the project's tasks with each one's
// dot and count; tasksRail follows the project's feed and keeps a shared
// store of the project's tasks and per-task activity. Each link
// (tasksRailLink) binds its dot and count to it, and the board (tasksBoard)
// draws its columns from it — so a session started anywhere, or a task an
// agent moves from its session, shows as it happens.
//
//   <nav x-data="tasksRail" data-seed="rail-seed" data-reports-feed>
//     <a x-data="tasksRailLink" data-task-id="t1"> … <span x-show="hasDot" …>
//
// data-reports-feed marks the rail as the page's feed (the project page):
// it then reports the connection to the stream bar as `feed-status`. The
// task page's own feed reports there instead.

import { FeedStatus } from "../../../../shared/domain/feed.js";
import { readSeed } from "../../../../shared/presentation/seed.js";
import { activityByTask, applyRailChange, linkState } from "../../domain/activity.js";
import { applyTaskChange } from "../../domain/board.js";

/**
 * The store the rail writes and its links and the board read: Alpine.store("tasksRail").
 * @typedef {{
 *   byTask: Record<string, import("../../domain/activity.js").Activity>,
 *   tasks: import("../../domain/task.js").Task[],
 *   seeded: boolean,
 * }} RailStore
 */

/** @param {{ gateway: import("../../domain/ports.js").RailGateway, store: RailStore }} deps */
export const rail = ({ gateway, store }) => () => {
  let unfollow = () => {};
  let sessions = [];

  return {
    projectId: "",
    /** This rail is the page's feed (the project page): it reports the connection to the stream bar. */
    reportsFeed: false,

    init() {
      this.reportsFeed = this.$el.dataset.reportsFeed !== undefined;
      let seed;
      try {
        seed = gateway.decodeSeed(readSeed(this.$el));
      } catch {
        return; // the server's rendering stays as it is
      }
      this.projectId = seed.projectId;
      sessions = seed.sessions;
      store.tasks = seed.tasks;
      store.seeded = true;
      this.recount();
      unfollow = gateway.follow(
        this.projectId,
        (change) => this.apply(change),
        (status) => this.feedStatus(status),
      );
    },

    feedStatus(status) {
      if (this.reportsFeed) this.$dispatch("feed-status", status);
      if (status === FeedStatus.RESYNCED) this.resync();
    },

    /** One feed change: a task's, or a session's. */
    apply(change) {
      if (change.kind === "task-upsert" || change.kind === "task-deleted") {
        const id = change.kind === "task-deleted" ? change.id : change.task.id;
        const previous = store.tasks.find((t) => t.id === id) ?? null;
        store.tasks = applyTaskChange(store.tasks, change);
        this.$dispatch("task-changed", { change, previous });
        return;
      }
      sessions = applyRailChange(sessions, change);
      this.recount();
    },

    /**
     * Changes were missed while the stream was down: read everything again.
     * Sessions and tasks are refreshed independently, so one list failing
     * keeps the other current; whatever fails keeps what it shows until the
     * next resync.
     */
    async resync() {
      const [list, tasks] = await Promise.allSettled([gateway.listSessions(this.projectId), gateway.listTasks(this.projectId)]);
      if (list.status === "fulfilled") {
        sessions = list.value;
        this.recount();
      }
      if (tasks.status === "fulfilled") store.tasks = tasks.value;
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
