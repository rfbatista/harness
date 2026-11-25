// The project's feed, headless. tasksRailFeed follows the project's feed for
// the page and keeps Alpine.store("tasksRail"): the project's tasks, the
// per-task activity, the archived count and the latest status moves. The
// board, the archived and feature pages and the task editor read that store,
// and the page hears the feed through the `task-changed`, `feed-change` and
// `feed-status` window events. It draws nothing: the rail is drawn from the
// server's seed and the feed by the React island, which does not read this
// store (ADR 0002).
//
//   <div hidden x-data="tasksRailFeed" data-seed="rail-seed" data-reports-feed></div>
//
// data-reports-feed marks the page's feed (the project page): it then reports
// the connection to the stream bar as `feed-status`. The task page's own feed
// reports there instead.

import { FeedStatus } from "../../../../shared/domain/feed.js";
import { readSeed } from "../../../../shared/presentation/seed.js";
import { activityByTask, applyRailChange } from "../../domain/activity.js";
import { applyTaskChange, onBoard } from "../../domain/board.js";

/**
 * The store the feed owner writes: Alpine.store("tasksRail").
 * @typedef {{
 *   byTask: Record<string, import("../../domain/activity.js").Activity>,
 *   tasks: import("../../domain/task.js").Task[],
 *   seeded: boolean,
 *   projectId?: string,
 *   drawerOpen: boolean,  the phone drawer is open (the old rail's; the island keeps its own)
 *   statusChanges?: Record<string, import("../../domain/board.js").StatusChange>,  the latest move per task seen over the feed
 *   archivedCount?: number,  the project's archived tasks, for "Archived (N)"
 *   observer?: { before(change: object): void, after(change: object, previous: object | null): void },  the old rail's view of each change, until the island replaces it
 * }} RailStore
 */

/**
 * @param {{
 *   gateway: import("../../domain/ports.js").RailGateway,
 *   store: RailStore,
 * }} deps
 */
export const railFeed = ({ gateway, store }) => () => {
  let unfollow = () => {};
  let sessions = [];
  /** The feed has been live once: changes before then (a merge state computed while the page loaded) were never delivered. */
  let wentLive = false;

  return {
    projectId: "",
    /** This is the page's feed (the project page): it reports the connection to the stream bar. */
    reportsFeed: false,

    init() {
      this.reportsFeed = this.$el.dataset.reportsFeed !== undefined;
      let seed;
      try {
        seed = gateway.decodeSeed(readSeed(this.$el));
      } catch {
        return; // the store stays unseeded
      }
      this.projectId = seed.projectId;
      sessions = seed.sessions;
      store.projectId = seed.projectId;
      store.tasks = onBoard(seed.tasks);
      store.archivedCount = seed.archivedCount;
      store.seeded = true;
      this.recount();
      unfollow = gateway.follow(
        this.projectId,
        (change) => this.apply(change),
        (status) => this.feedStatus(status),
      );
    },

    /**
     * The feed's state. On its first live, and on every resync, the lists are
     * read again: the seed was painted before the stream opened, and what the
     * server announced in between (a merge state it had just computed) never
     * reached this page.
     */
    feedStatus(status) {
      if (this.reportsFeed) this.$dispatch("feed-status", status);
      const first = status === FeedStatus.LIVE && !wentLive;
      if (status === FeedStatus.LIVE) wentLive = true;
      if (first || status === FeedStatus.RESYNCED) this.resync();
    },

    /** One feed change: a task's, or a session's. */
    apply(change) {
      // The project changed under the pages fetched ahead of a click.
      this.$dispatch("feed-change");
      if (change.kind === "task-status") {
        store.statusChanges = { ...store.statusChanges, [change.change.taskId]: change.change };
        return;
      }
      store.observer?.before(change);
      let previous = null;
      if (change.kind === "task-upsert" || change.kind === "task-deleted") {
        const id = change.kind === "task-deleted" ? change.id : change.task.id;
        previous = store.tasks.find((t) => t.id === id) ?? null;
        store.tasks = applyTaskChange(store.tasks, change);
        this.countArchived(change, previous);
        this.$dispatch("task-changed", { change, previous });
      } else {
        sessions = applyRailChange(sessions, change);
        this.recount();
      }
      store.observer?.after(change, previous);
    },

    /**
     * Changes were missed while the stream was down: read everything again.
     * Sessions and tasks are refreshed independently, so one list failing
     * keeps the other current; whatever fails keeps what it shows until the
     * next resync. A resync is not news: no wash, no announcement.
     */
    async resync() {
      const [list, tasks] = await Promise.allSettled([gateway.listSessions(this.projectId), gateway.listTasks(this.projectId)]);
      if (list.status === "fulfilled") {
        sessions = list.value;
        this.recount();
      }
      if (tasks.status === "fulfilled") store.tasks = onBoard(tasks.value);
      this.recountArchived();
    },

    /**
     * Keeps "Archived (N)" in step: a task the store had that arrives
     * archived adds one. A done task the store did not have may be a restore
     * (or a new task); only the server knows, so the count is read again. A
     * restored feature brings back the children archived with it over the
     * feed, but not the ones archived on their own before it, which its card
     * lists: the task list is read again too.
     */
    countArchived(change, previous) {
      if (change.kind !== "task-upsert") return;
      if (change.task.archivedAt && previous) store.archivedCount += 1;
      else if (!change.task.archivedAt && !previous && change.task.status === "done") {
        this.recountArchived();
        this.reloadTasks();
      }
    },

    /** Reads the project's tasks again, what resync does for them alone. */
    async reloadTasks() {
      try {
        store.tasks = onBoard(await gateway.listTasks(this.projectId));
      } catch {
        // keeps what it shows until the next change or resync
      }
    },

    async recountArchived() {
      try {
        store.archivedCount = (await gateway.listArchived(this.projectId)).length;
      } catch {
        // keeps the count it shows until the next change or resync
      }
    },

    recount() {
      store.byTask = activityByTask(sessions);
    },

    destroy() {
      unfollow();
      // The model is the project's whose feed this was; another frame's
      // (or none) starts from its own seed.
      store.tasks = [];
      store.byTask = {};
      store.seeded = false;
      store.statusChanges = {};
      store.archivedCount = 0;
      store.drawerOpen = false;
    },
  };
};
