// The project root's kanban board. It renders the rail's store (the project's
// tasks and per-task activity, kept live by tasksRail over the project feed),
// so a task an agent creates or moves shows here as it happens. A card opens
// the task.
//
//   <main x-data="tasksBoard" data-project-id="p1" x-on:task-changed.window="taskChanged">
//     <div x-ignore data-ssr>…first paint…</div>
//     <template x-if="hasTasks"> … x-for="column in columns" …

import { toColumns } from "../boardView.js";

/** @param {{ store: import("./rail.js").RailStore }} deps */
export const board = ({ store }) => () => ({
  projectId: "",
  /** The live template has rendered and the server copy is gone. */
  ready: false,
  error: null,
  /** Read out by the polite live region. */
  announcement: "",
  /** Tasks that just arrived or moved over the feed. */
  freshIds: [],

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

  /** A task changed over the feed (tasksRail dispatched it). Until the feed carries ticket changes there is nothing to do; the markup binds it from the start. */
  taskChanged() {},

  dismissError() {
    this.error = null;
  },
});
