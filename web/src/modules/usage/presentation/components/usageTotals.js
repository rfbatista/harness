// A task's and its sessions' token totals on the task page: one watch that
// seeds a shared store and follows the project's usage stream, and small
// chips that read it.
//
//   <div x-data="usageTotalsWatch" data-seed="usage-totals-seed" hidden></div>
//   <span x-data="usageTotalsChip" x-show="hasTask" x-text="taskText" x-bind:title="taskTitle"></span>
//   <span x-data="usageTotalsChip" x-show="hasSession(selected.id)" x-text="sessionText(selected.id)" …></span>
//
// The chips take the session id as an argument, so the session head's chip
// follows the selection. A page without the seed shows no chip.

import { FeedStatus } from "../../../../shared/domain/feed.js";
import { readSeed } from "../../../../shared/presentation/seed.js";
import { addLiveRecord, startTotals } from "../../domain/totals.js";
import { totalsChip } from "../totals.js";

/** @typedef {{ totals: import("../../domain/totals.js").LiveTotals|null }} TotalsStore */

/** @returns {TotalsStore} */
export const emptyTotalsStore = () => ({ totals: null });

/**
 * @param {{ gateway: import("../../domain/ports.js").UsageGateway, store: TotalsStore }} deps
 */
export const usageTotalsWatch = ({ gateway, store }) => () => {
  let unfollow = () => {};

  return {
    init() {
      let seed;
      try {
        seed = gateway.decodeTotalsSeed(readSeed(this.$el));
      } catch {
        return; // no chips; the rest of the page is unaffected
      }
      store.totals = startTotals(seed);
      unfollow = gateway.follow(
        seed.projectId,
        (record) => {
          store.totals = addLiveRecord(store.totals, record);
        },
        (status) => status === FeedStatus.RESYNCED && this.reread(),
      );
    },

    destroy() {
      unfollow();
    },

    /** Records may have been missed while the stream was down: read the totals again. */
    async reread() {
      const t = store.totals;
      if (!t) return;
      try {
        const task = t.taskId ? await gateway.totals({ taskId: t.taskId }) : null;
        const ids = Object.keys(t.sessions);
        const sessions = await Promise.all(ids.map((id) => gateway.totals({ sessionId: id })));
        store.totals = {
          ...store.totals,
          task: t.taskId ? task : store.totals.task,
          sessions: { ...store.totals.sessions, ...Object.fromEntries(ids.map((id, i) => [id, sessions[i]])) },
        };
      } catch {
        // the totals stay as they were until the next record or reconnect
      }
    },
  };
};

/** @param {{ store: TotalsStore }} deps */
export const usageTotalsChip = ({ store }) => () => ({
  get taskChip() {
    return totalsChip(store.totals?.task);
  },
  get hasTask() {
    return this.taskChip !== null;
  },
  get taskText() {
    return this.taskChip?.text ?? "";
  },
  get taskTitle() {
    return this.taskChip?.title ?? "";
  },
  sessionChip(id) {
    return id ? totalsChip(store.totals?.sessions[id]) : null;
  },
  hasSession(id) {
    return this.sessionChip(id) !== null;
  },
  sessionText(id) {
    return this.sessionChip(id)?.text ?? "";
  },
  sessionTitle(id) {
    return this.sessionChip(id)?.title ?? "";
  },
});
