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
import { applyTaskChange } from "../../domain/board.js";
import { toColumns } from "../boardView.js";

/**
 * @param {{
 *   gateway: import("../../domain/ports.js").TaskGateway,
 *   store: import("./rail.js").RailStore,
 * }} deps
 */
export const board = ({ gateway, store }) => () => ({
  projectId: "",
  /** The live template has rendered and the server copy is gone. */
  ready: false,
  error: null,
  /** Read out by the polite live region. */
  announcement: "",
  /** Tasks that just arrived or moved over the feed. */
  freshIds: [],
  /** The status each in-flight or just-finished move asked for, by task id: the latest wins. */
  moves: {},

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
    const { taskId } = event.target.dataset;
    const status = event.target.value;
    const task = store.tasks.find((t) => t.id === taskId);
    if (!task || task.status === status) return;
    this.moves = { ...this.moves, [taskId]: status };
    this.error = null;
    store.tasks = applyTaskChange(store.tasks, { kind: "task-upsert", task: { ...task, status } });
    try {
      const saved = await gateway.moveTask(taskId, status);
      if (this.moves[taskId] !== status) return; // a later move of this card is on its way
      store.tasks = applyTaskChange(store.tasks, { kind: "task-upsert", task: saved });
    } catch (err) {
      if (this.moves[taskId] !== status) return;
      store.tasks =
        codeOf(err) === Codes.TICKET_NOT_FOUND
          ? applyTaskChange(store.tasks, { kind: "task-deleted", id: taskId })
          : applyTaskChange(store.tasks, { kind: "task-upsert", task });
      this.error = describeError(err);
    }
  },

  /** A task changed over the feed (tasksRail dispatched it). Until the feed carries ticket changes there is nothing to do; the markup binds it from the start. */
  taskChanged() {},

  dismissError() {
    this.error = null;
  },
});
