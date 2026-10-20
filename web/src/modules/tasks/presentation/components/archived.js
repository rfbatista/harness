// The Archived view: the project's archived tasks, with Restore and Delete
// (ADR 0014). It starts from the page's seed and keeps in step with the
// project feed through the rail, which dispatches task-changed for every
// ticket change. A row that was restored or deleted, here or elsewhere,
// collapses to a receipt until the person leaves, so the list never jumps
// under the pointer.
//
//   <main x-data="tasksArchived" data-seed="archived-seed" x-on:task-changed.window="taskChanged">
//     <ul data-archived-list> … x-for="row in rows" … <li x-bind:data-task-id="row.id">
//       <button x-on:click="restore">Restore</button> <button x-on:click="askDelete">Delete</button>

import { describeError } from "../../../../shared/presentation/errors.js";
import { readSeed } from "../../../../shared/presentation/seed.js";
import { archivedSummary, toArchivedRows } from "../archivedView.js";

const rowOf = (event) => event.target.closest?.("[data-task-id]")?.dataset.taskId ?? "";

/** @param {{ gateway: import("../../domain/ports.js").TaskGateway }} deps */
export const archived = ({ gateway }) => () => {
  /** The component's root: $el inside an event handler is the element that fired. */
  let root = null;

  return {
    projectId: "",
    /** @type {import("../../domain/task.js").Task[]} the archived tasks, and the ones that left with a receipt */
    tasks: [],
    /** @type {Record<string, "restored" | "deleted">} */
    receipts: {},
    /** @type {Record<string, "restoring" | "deleting">} */
    busy: {},
    confirming: "",
    errors: {},
    announcement: "",
    error: null,
    /** The live list has taken over from the server's copy. */
    ready: false,

    get rows() {
      return toArchivedRows(this.tasks, { projectId: this.projectId, receipts: this.receipts, busy: this.busy, confirming: this.confirming, errors: this.errors });
    },
    get count() {
      return this.tasks.filter((t) => !this.receipts[t.id]).length;
    },
    get summary() {
      return archivedSummary(this.count);
    },
    get boardHref() {
      return `/projects/${encodeURIComponent(this.projectId)}`;
    },
    get isEmpty() {
      return this.tasks.length === 0;
    },

    init() {
      root = this.$el;
      try {
        const seed = readSeed(this.$el);
        this.projectId = seed.project_id ?? "";
        this.tasks = (seed.tickets ?? []).flatMap((dto) => {
          try {
            return [gateway.decodeTask(dto)];
          } catch {
            return [];
          }
        });
      } catch (err) {
        this.error = describeError(err);
        return; // the server's rendering stays as it is
      }
      this.ready = true;
      this.$nextTick(() => {
        for (const node of this.$el.querySelectorAll("[data-ssr]")) node.remove();
      });
    },

    async restore(event) {
      const id = rowOf(event);
      const task = this.tasks.find((t) => t.id === id);
      if (!task || this.busy[id]) return;
      await this.act(id, "restoring", () => gateway.restoreTask(id), "restored");
      if (this.receipts[id]) this.announcement = `Restored ${task.title} to Done.`;
    },

    askDelete(event) {
      this.confirming = rowOf(event);
      this.$nextTick(() => root?.querySelector('[data-confirm="delete"] [data-cancel]')?.focus());
    },

    cancelDelete() {
      const id = this.confirming;
      this.confirming = "";
      this.$nextTick(() => root?.querySelector(`[data-task-id="${CSS.escape(id)}"] [data-delete]`)?.focus());
    },

    async confirmDelete(event) {
      const id = rowOf(event) || this.confirming;
      const task = this.tasks.find((t) => t.id === id);
      if (!task) return;
      this.confirming = "";
      await this.act(id, "deleting", () => gateway.deleteTask(id), "deleted");
      if (!this.receipts[id]) return;
      this.announcement = `Deleted ${task.title}.`;
      // The focus goes on to the next row still archived.
      const next = this.rows.find((r) => !r.receipt && r.id !== id);
      this.$nextTick(() => (next ? root?.querySelector(`[data-task-id="${CSS.escape(next.id)}"] [data-restore]`) : null)?.focus());
    },

    /** Runs a row's action: busy meanwhile, its receipt on success, its error under it on failure. */
    async act(id, doing, send, done) {
      this.busy = { ...this.busy, [id]: doing };
      const { [id]: _, ...rest } = this.errors;
      this.errors = rest;
      try {
        await send();
        this.receipts = { ...this.receipts, [id]: done };
      } catch (err) {
        this.errors = { ...this.errors, [id]: describeError(err) };
      } finally {
        const { [id]: __, ...still } = this.busy;
        this.busy = still;
      }
    },

    /**
     * A ticket changed over the feed: one archived elsewhere joins the list;
     * one restored or deleted elsewhere leaves it with its receipt. This
     * page's own actions already have theirs.
     */
    taskChanged(event) {
      const { change } = event.detail;
      const id = change.kind === "task-deleted" ? change.id : change.task.id;
      if (change.kind === "task-upsert" && change.task.archivedAt) {
        const { [id]: _, ...rest } = this.receipts;
        this.receipts = rest;
        this.tasks = [...this.tasks.filter((t) => t.id !== id), change.task];
        return;
      }
      if (!this.tasks.some((t) => t.id === id) || this.receipts[id] || this.busy[id]) return;
      this.receipts = { ...this.receipts, [id]: change.kind === "task-deleted" ? "deleted" : "restored" };
    },
  };
};
