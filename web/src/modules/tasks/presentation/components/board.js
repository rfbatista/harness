// The project root's kanban board. It renders the rail's store (the project's
// tasks and per-task activity, kept live by tasksRail over the project feed),
// so a task an agent creates or moves shows here as it happens. A card opens
// the task; its select moves it, optimistically, with a status-only update.
//
//   <main x-data="tasksBoard" data-project-id="p1" x-on:task-changed.window="taskChanged">
//     <div x-ignore data-ssr>…first paint…</div>
//     <template x-if="hasTasks"> … x-for="column in columns" …
//       <select data-task-id="t1" x-on:change="moveTo">

import { Codes, codeOf } from "../../../../shared/domain/errors.js";
import { describeError } from "../../../../shared/presentation/errors.js";
import { applyTaskChange, describeChange } from "../../domain/board.js";
import { toColumns } from "../boardView.js";

/** How long a task that arrived or moved over the feed stays highlighted. */
export const FRESH_MS = 8_000;

/**
 * @param {{
 *   gateway: import("../../domain/ports.js").TaskGateway,
 *   store: import("./rail.js").RailStore,
 *   setTimeout?: typeof globalThis.setTimeout,
 * }} deps
 */
export const board = ({ gateway, store, setTimeout = globalThis.setTimeout.bind(globalThis) }) => () => {
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

    get columns() {
      return toColumns(store.tasks, { projectId: this.projectId, byTask: store.byTask, fresh: new Set(this.freshIds) });
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
      if (!store.seeded) return; // the server's rendering stays as it is
      // Alpine has rendered the live board; drop the server-rendered copy.
      this.$nextTick(() => {
        for (const node of this.$el.querySelectorAll("[data-ssr]")) node.remove();
        this.ready = true;
      });
    },

    /** The card's select changed: move the task, optimistically. */
    async moveTo(event) {
      const select = event.target;
      const { taskId } = select.dataset;
      const status = select.value;
      const task = store.tasks.find((t) => t.id === taskId);
      if (!task || task.status === status) return;
      const focused = select.ownerDocument?.activeElement === select;
      const move = this.moves[taskId] ?? { base: task.status, pending: [] };
      this.moves = { ...this.moves, [taskId]: { base: move.base, pending: [...move.pending, status] } };
      delete this.echoes[taskId];
      this.error = null;
      this.show(taskId, status);
      if (focused) this.refocus(taskId);
      try {
        const saved = await gateway.moveTask(taskId, status);
        const { newest } = this.settle(taskId, status);
        if (newest) {
          this.show(taskId, newest); // a later move of this card is still on its way
          return;
        }
        store.tasks = applyTaskChange(store.tasks, { kind: "task-upsert", task: saved });
        this.expectEcho(taskId, saved.status);
        this.announcement = describeChange(task, { kind: "task-upsert", task: saved });
      } catch (err) {
        const { newest, base } = this.settle(taskId, status);
        if (codeOf(err) === Codes.TICKET_NOT_FOUND) {
          store.tasks = applyTaskChange(store.tasks, { kind: "task-deleted", id: taskId });
          delete this.moves[taskId];
        } else if (newest) {
          this.show(taskId, newest);
        } else if (store.tasks.find((t) => t.id === taskId)?.status === status) {
          this.show(taskId, base); // nobody else changed it meanwhile: back to what the server holds
        }
        this.error = describeError(err);
        if (focused) this.refocus(taskId);
      }
    },

    /** Puts a status on a card, keeping the rest of the task as the store has it. */
    show(taskId, status) {
      const task = store.tasks.find((t) => t.id === taskId);
      if (task && task.status !== status) store.tasks = applyTaskChange(store.tasks, { kind: "task-upsert", task: { ...task, status } });
    },

    /**
     * One of a card's moves is settled (answered, failed, or echoed): takes it
     * off the pending list. Returns the newest still-pending status, if any,
     * and the status the server last confirmed.
     */
    settle(taskId, status) {
      const move = this.moves[taskId];
      if (!move) return { newest: null, base: undefined };
      const pending = [...move.pending];
      const at = pending.indexOf(status);
      if (at >= 0) pending.splice(at, 1);
      if (pending.length === 0) {
        delete this.moves[taskId];
        return { newest: null, base: move.base };
      }
      this.moves = { ...this.moves, [taskId]: { base: move.base, pending } };
      return { newest: pending.at(-1), base: move.base };
    },

    /** The server confirmed a move; its echo on the feed, should it still come, is not news. */
    expectEcho(taskId, status) {
      this.echoes = { ...this.echoes, [taskId]: status };
      setTimeout(() => {
        if (this.echoes[taskId] === status) delete this.echoes[taskId];
      }, FRESH_MS);
    },

    /** Keeps the keyboard on the card's select after Alpine re-rendered it in another column. */
    refocus(taskId) {
      this.$nextTick(() => {
        const select = root?.querySelector(`.card[data-task-id="${CSS.escape(taskId)}"] select`);
        select?.focus();
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
      const move = this.moves[id];
      if (move?.pending.includes(status)) {
        const { newest } = this.settle(id, status);
        if (newest) {
          this.moves = { ...this.moves, [id]: { ...this.moves[id], base: status } };
          this.show(id, newest);
        }
        return;
      }
      if (this.echoes[id] === status) {
        delete this.echoes[id];
        return;
      }
      if (move) this.moves = { ...this.moves, [id]: { ...move, base: status } }; // someone else moved it: that is what the server holds now
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
