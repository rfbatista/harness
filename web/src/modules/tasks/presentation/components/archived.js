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
import { afterClear, archivedSummary, clearScopeIds, toArchivedRows, toClearStrip } from "../archivedView.js";
import { taskHref } from "../boardView.js";

const rowOf = (event) => event.target.closest?.("[data-task-id]")?.dataset.taskId ?? "";

/** @param {{ gateway: import("../../domain/ports.js").TaskGateway }} deps */
export const archived = ({ gateway }) => () => {
  /** The component's root: $el inside an event handler is the element that fired. */
  let root = null;
  /** Which ask the confirm is for: an answer that arrives after the confirm was closed or reopened is dropped. (this.clear is a proxy, so it cannot be compared with the object it wraps.) */
  let asking = 0;

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
    /** @type {Record<string, number>} worktrees on record by task, from the store: the page never asks git for them */
    worktrees: {},
    /** The Clear worktrees confirm: null while closed. */
    clear: null,

    get rows() {
      return toArchivedRows(this.tasks, { projectId: this.projectId, receipts: this.receipts, busy: this.busy, confirming: this.confirming, errors: this.errors, worktrees: this.worktrees });
    },
    /** No archived task has a worktree on record. */
    get noClearable() {
      return this.tasks.every((t) => this.receipts[t.id] || (this.worktrees[t.id] ?? 0) === 0);
    },
    get clearOpen() {
      return this.clear !== null;
    },
    get strip() {
      return toClearStrip(this.clear ?? { label: "", phase: "loading", preview: null, result: null, error: null });
    },
    get stripHasLines() {
      return this.strip.lines.length > 0;
    },
    get stripCannotConfirm() {
      return !this.strip.canConfirm;
    },
    get stripHasError() {
      return this.strip.error !== null;
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
        this.worktrees = seed.worktrees ?? {};
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

    /** Clear worktrees from the header: every archived task of the project. */
    askClearAll() {
      const live = this.tasks.filter((t) => !this.receipts[t.id]);
      return this.askClear({ projectId: this.projectId }, live.length === 1 ? live[0].title : "all archived tasks", "[data-clear-all]");
    },

    /** Clear worktrees from a row: that task, and the children archived with a feature. */
    askClearRow(event) {
      const id = rowOf(event);
      const task = this.tasks.find((t) => t.id === id);
      if (!task) return;
      return this.askClear({ taskId: id }, task.title, `[data-task-id="${CSS.escape(id)}"] [data-clear]`);
    },

    async askClear(scope, label, opener) {
      const ask = ++asking;
      const clear = { scope, label, opener, phase: "loading", preview: null, result: null, error: null };
      this.clear = clear;
      this.$nextTick(() => root?.querySelector("[data-confirm='clear-worktrees'] [data-cancel]")?.focus());
      try {
        const preview = await gateway.previewArchivedWorktrees(scope);
        if (ask === asking) this.clear = { ...clear, phase: "ready", preview };
      } catch (err) {
        if (ask === asking) this.clear = { ...clear, phase: "error", error: describeError(err) };
      }
    },

    closeClear() {
      const opener = this.clear?.opener;
      asking++;
      this.clear = null;
      this.$nextTick(() => root?.querySelector(opener)?.focus());
    },

    async confirmClear() {
      const clear = this.clear;
      if (!clear || clear.phase !== "ready") return;
      this.clear = { ...clear, phase: "clearing" };
      try {
        const result = await gateway.clearArchivedWorktrees(clear.scope);
        const ids = clear.scope.taskId ? clearScopeIds(this.tasks, clear.scope.taskId) : this.tasks.map((t) => t.id);
        this.worktrees = afterClear(this.worktrees, ids, result.skipped);
        this.clear = { ...clear, phase: "done", result };
        this.announcement = `Cleared ${result.cleared} ${result.cleared === 1 ? "worktree" : "worktrees"}.`;
      } catch (err) {
        this.clear = { ...clear, phase: "error", error: describeError(err) };
      }
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
        // A child of an archived feature is refused with the feature, so the row can link to it.
        const feature = err?.details?.feature;
        this.errors = { ...this.errors, [id]: { ...describeError(err), featureHref: feature ? taskHref(this.projectId, feature.id) : "" } };
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
