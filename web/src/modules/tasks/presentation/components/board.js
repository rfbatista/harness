// The project root's kanban board. It renders the rail's store (the project's
// tasks and per-task activity, kept live by tasksRail over the project feed),
// so a task an agent creates or moves shows here as it happens. A card opens
// the task; its "Move to…" menu moves it, optimistically, with a status-only
// update.
//
//   <main x-data="tasksBoard" data-project-id="p1" x-on:task-changed.window="taskChanged">
//     <div x-ignore data-ssr>…first paint…</div>
//     <template x-if="hasTasks"> … x-for="column in columns" …
//       <article data-task-id="t1"> … <div class="menu" x-data="menu" x-on:menu-select="menuSelect">

import { Codes, codeOf } from "../../../../shared/domain/errors.js";
import { describeError } from "../../../../shared/presentation/errors.js";
import { applyTaskChange, describeChange } from "../../domain/board.js";
import { STATUSES } from "../../domain/task.js";
import { archiveAllView, toColumns } from "../boardView.js";

/** How long a task that arrived or moved over the feed stays highlighted. */
export const FRESH_MS = 8_000;

const statusLabel = (status) => STATUSES.find((s) => s.value === status)?.label ?? status;

/** How long a card that went back after a refused move says "Move failed". */
export const FAILED_MS = 4_000;

/** How often the cards' time in column refreshes. */
export const AGE_TICK_MS = 60_000;

/**
 * @param {{
 *   gateway: import("../../domain/ports.js").TaskGateway,
 *   store: import("./rail.js").RailStore,
 *   setTimeout?: typeof globalThis.setTimeout,
 *   clock?: { now: () => Date },
 * }} deps
 */
export const board = ({ gateway, store, setTimeout = globalThis.setTimeout.bind(globalThis), clock = { now: () => new Date() } }) => () => {
  /** The component's root (<main>): $el inside an event handler is the element that fired, not the root. */
  let root = null;

  return {
    projectId: "",
    /** The live template has rendered and the server copy is gone. */
    ready: false,
    error: null,
    /** Read out by the polite live region. */
    announcement: "",
    /** Tasks that just arrived or moved over the feed. */
    freshIds: [],
    /** Cards that went back after a refused move, for FAILED_MS. */
    failedIds: [],
    /**
     * This board's own moves per card, so a response, a failure and the
     * feed's echo are told apart from other people's changes: `base` is the
     * status the server last confirmed, `pending` the statuses asked for and
     * not yet confirmed, oldest first. The server publishes a move before it
     * answers, so an echo usually arrives before, or with, the response.
     * @type {Record<string, { base: string, pending: string[] }>}
     */
    moves: {},
    /** A move's confirmed status whose echo may still arrive, per card. */
    echoes: {},
    /** "Archive all": rest, confirm, busy, done or failed; archived is how many the last one took. */
    archiveAllPhase: "rest",
    archivedByAll: 0,
    /** Why the last "Archive all" failed, for the strip. */
    archiveAllError: null,
    /** "Archived (N)" as the server painted it, until the rail's store has its own count. */
    ssrArchivedCount: 0,
    /** What the cards' time in column is measured against; it moves once a minute, so nothing reorders. */
    now: clock.now(),

    get columns() {
      const pending = Object.fromEntries(Object.entries(this.moves).map(([id, m]) => [id, m.pending.at(-1)]));
      const columns = toColumns(store.tasks, { projectId: this.projectId, byTask: store.byTask, fresh: new Set(this.freshIds), now: this.now, pending, failed: new Set(this.failedIds) });
      // Done carries "Archive all": its strip, its fading cards and the receipt in place of the empty text.
      const all = this.archiveAll;
      return columns.map((c) => {
        const isDone = c.status === "done";
        const receipt = isDone ? all.emptyText : "";
        return { ...c, isDone, archiving: isDone && all.busy, receipt, showsEmpty: c.isEmpty && !receipt };
      });
    },
    get archiveAll() {
      const done = store.tasks.filter((t) => t.status === "done").length;
      return archiveAllView({ phase: this.archiveAllPhase, done, archived: this.archivedByAll });
    },
    get archivedLink() {
      return `Archived (${store.seeded ? (store.archivedCount ?? 0) : this.ssrArchivedCount})`;
    },
    get archivedHref() {
      return `/projects/${encodeURIComponent(this.projectId)}/archived`;
    },
    get hasTasks() {
      return store.seeded && store.tasks.length > 0;
    },
    get isEmpty() {
      return store.seeded && store.tasks.length === 0;
    },

    init() {
      root = this.$el;
      this.projectId = this.$el.dataset.projectId ?? "";
      this.ssrArchivedCount = Number(this.$el.dataset.archivedCount ?? 0);
      const tick = () => {
        this.now = clock.now();
        setTimeout(tick, AGE_TICK_MS);
      };
      setTimeout(tick, AGE_TICK_MS);
      if (!store.seeded) return; // the server's rendering stays as it is
      // Alpine has rendered the live board; drop the server-rendered copy.
      this.$nextTick(() => {
        for (const node of this.$el.querySelectorAll("[data-ssr]")) node.remove();
        this.ready = true;
      });
    },

    /** A card's "Move to…" menu chose an item. */
    menuSelect(event) {
      const { action, value } = event.detail;
      const taskId = event.target.closest?.("[data-task-id]")?.dataset.taskId;
      if (action === "move" && taskId) this.move(taskId, value, { refocus: true });
      if (action === "archive" && taskId) this.archive(taskId);
    },

    /**
     * Archives a done card: it leaves the board at once and comes back where
     * it was if the server refuses. The focus goes to the next card in Done,
     * else to "Archive all", else to "Archived (N)".
     */
    async archive(taskId) {
      const at = store.tasks.findIndex((t) => t.id === taskId);
      if (at < 0) return;
      const task = store.tasks[at];
      const done = this.columns.find((c) => c.status === "done").cards.map((c) => c.id);
      const i = done.indexOf(taskId);
      const next = done[i + 1] ?? done[i - 1] ?? "";
      this.error = null;
      store.tasks = store.tasks.filter((t) => t.id !== taskId);
      this.$nextTick(() => {
        const target =
          (next && root?.querySelector(`.card[data-task-id="${CSS.escape(next)}"] .move-to`)) ||
          root?.querySelector("[data-archive-all]") ||
          root?.querySelector(".archived-link");
        target?.focus();
      });
      try {
        await gateway.archiveTask(taskId);
        store.archivedCount = (store.archivedCount ?? 0) + 1;
        this.announcement = `Archived ${task.title}.`;
      } catch (err) {
        if (!store.tasks.some((t) => t.id === taskId)) store.tasks = [...store.tasks.slice(0, at), task, ...store.tasks.slice(at)];
        this.error = describeError(err);
        this.announcement = `Couldn't archive ${task.title}.`;
      }
    },

    /**
     * Moves a card, optimistically: it lands on top of its new column at
     * once, faded until the server answers, and goes back if the server
     * refuses. The one path for the menu and a drop. refocus keeps the
     * keyboard on the card's "Move to…" in whichever column it ends up.
     */
    async move(taskId, status, { refocus = false } = {}) {
      const task = store.tasks.find((t) => t.id === taskId);
      if (!task || task.status === status) return;
      const move = this.moves[taskId] ?? { base: task.status, baseSince: task.statusSince, pending: [] };
      this.moves = { ...this.moves, [taskId]: { ...move, pending: [...move.pending, status] } };
      delete this.echoes[taskId];
      this.failedIds = this.failedIds.filter((x) => x !== taskId);
      this.error = null;
      this.show(taskId, status, clock.now().getTime());
      if (refocus) this.refocus(taskId);
      try {
        const saved = await gateway.moveTask(taskId, status);
        const { newest } = this.settle(taskId, status);
        if (newest) {
          this.show(taskId, newest); // a later move of this card is still on its way
          return;
        }
        store.tasks = applyTaskChange(store.tasks, { kind: "task-upsert", task: saved });
        this.expectEcho(taskId, saved.status);
        this.announcement = `Moved ${task.title} to ${statusLabel(saved.status)}.`;
      } catch (err) {
        const { newest, base, baseSince } = this.settle(taskId, status);
        if (codeOf(err) === Codes.TICKET_NOT_FOUND) {
          store.tasks = applyTaskChange(store.tasks, { kind: "task-deleted", id: taskId });
          delete this.moves[taskId];
        } else if (newest) {
          this.show(taskId, newest);
        } else if (store.tasks.find((t) => t.id === taskId)?.status === status) {
          this.show(taskId, base, baseSince); // nobody else changed it meanwhile: back to what the server holds
          this.flagFailed(taskId);
        }
        this.error = describeError(err);
        this.announcement = `Couldn't move ${task.title} to ${statusLabel(status)}.`;
        if (refocus) this.refocus(taskId);
      }
    },

    /** "Archive all" opens its in-page confirmation, its button focused. */
    askArchiveAll() {
      this.archiveAllPhase = "confirm";
      this.archiveAllError = null;
      this.$nextTick(() => root?.querySelector('[data-confirm="archive-all"] [data-variant="primary"]')?.focus());
    },

    cancelArchiveAll() {
      this.archiveAllPhase = "rest";
      this.$nextTick(() => root?.querySelector("[data-archive-all]")?.focus());
    },

    dismissArchiveAll() {
      this.archiveAllPhase = "rest";
      this.archiveAllError = null;
    },

    /**
     * Archives all of Done in one request. Not optimistic: the cards fade
     * until the server answers. A card the feed already took off was counted
     * by the rail; the rest are counted here, once.
     */
    async confirmArchiveAll() {
      if (this.archiveAllPhase === "busy") return;
      this.archiveAllPhase = "busy";
      this.archiveAllError = null;
      try {
        const { archived, ids } = await gateway.archiveDoneTasks(this.projectId);
        const gone = new Set(ids);
        const still = store.tasks.filter((t) => gone.has(t.id)).length;
        store.tasks = store.tasks.filter((t) => !gone.has(t.id));
        store.archivedCount = (store.archivedCount ?? 0) + still;
        this.archivedByAll = archived;
        this.archiveAllPhase = "done";
        this.announcement = `Archived ${archived} ${archived === 1 ? "task" : "tasks"}.`;
        this.$nextTick(() => root?.querySelector("[data-archived-receipt] a")?.focus());
      } catch (err) {
        this.archiveAllError = describeError(err);
        this.archiveAllPhase = "failed";
      }
    },

    /** Puts a status on a card, keeping the rest of the task as the store has it; since, when given, is when it entered it. */
    show(taskId, status, since) {
      const task = store.tasks.find((t) => t.id === taskId);
      if (!task || (task.status === status && since === undefined)) return;
      store.tasks = applyTaskChange(store.tasks, { kind: "task-upsert", task: { ...task, status, statusSince: since ?? task.statusSince } });
    },

    /** A card that went back after a refused move says so for a while. */
    flagFailed(taskId) {
      this.failedIds = [...this.failedIds, taskId];
      setTimeout(() => {
        this.failedIds = this.failedIds.filter((x) => x !== taskId);
      }, FAILED_MS);
    },

    /**
     * One of a card's moves is settled (answered, failed, or echoed): takes it
     * off the pending list. Returns the newest still-pending status, if any,
     * and the status the server last confirmed.
     */
    settle(taskId, status) {
      const move = this.moves[taskId];
      if (!move) return { newest: null, base: undefined, baseSince: undefined };
      const pending = [...move.pending];
      const at = pending.indexOf(status);
      if (at >= 0) pending.splice(at, 1);
      if (pending.length === 0) {
        const { [taskId]: _, ...rest } = this.moves;
        this.moves = rest;
        return { newest: null, base: move.base, baseSince: move.baseSince };
      }
      this.moves = { ...this.moves, [taskId]: { ...move, pending } };
      return { newest: pending.at(-1), base: move.base, baseSince: move.baseSince };
    },

    /** The server confirmed a move; its echo on the feed, should it still come, is not news. */
    expectEcho(taskId, status) {
      this.echoes = { ...this.echoes, [taskId]: status };
      setTimeout(() => {
        if (this.echoes[taskId] === status) delete this.echoes[taskId];
      }, FRESH_MS);
    },

    /** Keeps the keyboard on the card's "Move to…" after Alpine re-rendered it in another column. */
    refocus(taskId) {
      this.$nextTick(() => {
        root?.querySelector(`.card[data-task-id="${CSS.escape(taskId)}"] .move-to`)?.focus();
      });
    },

    /**
     * A task changed over the feed (tasksRail applied it and dispatched this):
     * say what happened and highlight the card for a while. The echo of a move
     * made from this board is neither announced nor highlighted; while a newer
     * move of the same card is pending, the card keeps that newer status.
     */
    taskChanged(event) {
      const { change, previous } = event.detail;
      if (change.kind === "task-deleted") {
        delete this.moves[change.id];
        delete this.echoes[change.id];
        this.announcement = describeChange(previous, change);
        return;
      }
      const { id, status } = change.task;
      if (change.task.archivedAt) {
        delete this.moves[id];
        delete this.echoes[id];
        // Not on this board before (its own archive's echo, or one it never showed): no news.
        if (previous) this.announcement = describeChange(previous, change);
        return;
      }
      const move = this.moves[id];
      if (move?.pending.includes(status)) {
        const { newest } = this.settle(id, status);
        if (newest) {
          this.moves = { ...this.moves, [id]: { ...this.moves[id], base: status, baseSince: change.task.statusSince } };
          this.show(id, newest);
        }
        return;
      }
      if (this.echoes[id] === status) {
        delete this.echoes[id];
        return;
      }
      if (move) this.moves = { ...this.moves, [id]: { ...move, base: status, baseSince: change.task.statusSince } }; // someone else moved it: that is what the server holds now
      this.announcement = describeChange(previous, change);
      this.freshIds = [...this.freshIds, id];
      setTimeout(() => {
        this.freshIds = this.freshIds.filter((x) => x !== id);
      }, FRESH_MS);
    },

    dismissError() {
      this.error = null;
    },
  };
};
