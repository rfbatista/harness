// The rail, live. The server renders the project's tasks in kanban groups;
// tasksRail follows the project's feed, keeps a shared store of the
// project's tasks and per-task activity, and renders the same groups from
// it, so a task an agent creates or moves from its session regroups the
// rail as it happens, and a session started anywhere shows on its link as
// it starts. The board (tasksBoard) draws its columns from the same store.
//
//   <nav x-data="tasksRail" data-seed="rail-seed" data-current-task="t1" data-reports-feed>
//     <div x-ignore data-ssr>…first paint…</div>
//     <template x-for="group in groups"> … x-for="link in group.links" …
//
// data-reports-feed marks the rail as the page's feed (the project page):
// it then reports the connection to the stream bar as `feed-status`. The
// task page's own feed reports there instead.

import { FeedStatus } from "../../../../shared/domain/feed.js";
import { readSeed } from "../../../../shared/presentation/seed.js";
import { activityByTask, applyRailChange, linkState } from "../../domain/activity.js";
import { applyTaskChange, byStatus, RAIL_ORDER } from "../../domain/board.js";
import { STATUSES } from "../../domain/task.js";
import { taskHref } from "../boardView.js";

const QUIET = Object.freeze({ live: 0, attention: false });

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
    /** The open task's id, marked aria-current; "" when none. */
    currentTaskId: "",
    /** This rail is the page's feed (the project page): it reports the connection to the stream bar. */
    reportsFeed: false,

    /** The rail's groups: non-empty statuses in RAIL_ORDER, each task with its dot and count. */
    get groups() {
      const cols = byStatus(store.tasks);
      return RAIL_ORDER.filter((st) => cols[st].length > 0).map((st) => ({
        key: st,
        label: STATUSES.find((s) => s.value === st).label.toLowerCase(),
        links: cols[st].map((t) => this.link(t)),
      }));
    },
    get isEmpty() {
      return store.seeded && store.tasks.length === 0;
    },
    /** Review requests waiting on the person across the project, for the Reviews link's badge. */
    get reviewsCount() {
      return store.tasks.reduce((n, t) => n + (t.pendingReviews ?? 0), 0);
    },
    get hasReviews() {
      return this.reviewsCount > 0;
    },

    init() {
      this.reportsFeed = this.$el.dataset.reportsFeed !== undefined;
      this.currentTaskId = this.$el.dataset.currentTask ?? "";
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
      // Alpine has rendered the live groups; drop the server-rendered copy.
      this.$nextTick(() => {
        for (const node of this.$el.querySelectorAll("[data-ssr]")) node.remove();
      });
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

    /** A task as the rail links it: its page, its dot and its count. */
    link(task) {
      const activity = store.byTask[task.id] ?? QUIET;
      const { state, word } = linkState(activity, task.pendingReviews);
      return {
        id: task.id,
        href: taskHref(this.projectId, task.id),
        label: task.title,
        current: task.id === this.currentTaskId ? "page" : false,
        state,
        word,
        hasDot: state !== "",
        live: activity.live,
        hasCount: activity.live > 0,
      };
    },

    destroy() {
      unfollow();
    },
  };
};
