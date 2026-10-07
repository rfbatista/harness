// The task in the task page's header: its title, status and description,
// edited in place, and deleted. Each edit sends only what changed: a status
// change is status-only and reloads the page, so the rail regroups the task;
// a text edit does not resend the status. A task with sessions is not deleted (they would be left
// under no task); the editor asks the server right before deleting it.
//
//   <div x-data="tasksTaskEditor" data-seed="task-seed"> … </div>

import { describeError } from "../../../../shared/presentation/errors.js";
import { readSeed } from "../../../../shared/presentation/seed.js";
import { deleteBlocker, titleProblem } from "../../domain/task.js";
import { statusChangeView } from "../statusChange.js";

const TICK_MS = 30_000;

/**
 * @param {{
 *   gateway: import("../../domain/ports.js").TaskGateway,
 *   navigate: (url: string) => void,
 *   reload: () => void,
 *   store?: import("./rail.js").RailStore,  the rail's live model: the task as the feed has it, and its latest move
 *   clock?: { now: () => Date },
 * }} deps
 */
export const taskEditor =
  ({ gateway, navigate, reload, store = { tasks: [], statusChanges: {} }, clock = { now: () => new Date() } }) =>
  () => {
    let ticker = null;
    return {
      /** @type {import("../../domain/task.js").Task | null} */
      task: null,
      now: clock.now(),
      /** This page is saving a status of its own: the feed's echo is not someone else's move. */
      movingStatus: false,
      status: "",
      editingTask: false,
      draftTitle: "",
      draftDescription: "",
      savingTask: false,
      confirmingTaskDelete: false,
      checkingTaskDelete: false,
      deletingTask: false,
      taskDeleteProblem: "",
      taskError: null,

      get title() {
        return this.task?.title ?? "";
      },
      get description() {
        return this.task?.description ?? "";
      },
      get hasDescription() {
        return this.description.trim() !== "";
      },
      /** The description shows under the title, except while it is edited. */
      get showsDescription() {
        return this.hasDescription && !this.editingTask;
      },
      get cannotSaveTask() {
        return this.savingTask || titleProblem(this.draftTitle) !== "";
      },

      /** The task as the feed has it now (the rail keeps it). */
      get storedTask() {
        return store.tasks.find((t) => t.id === this.task?.id) ?? null;
      },
      get storedStatus() {
        return this.storedTask?.status ?? "";
      },
      /** Who moved the task last, when and why: seen over the feed while the page is open. */
      get statusChange() {
        const change = this.task ? store.statusChanges?.[this.task.id] : null;
        if (!change) return null;
        const architectSessionId = this.storedTask?.architectSessionId || this.task.architectSessionId || "";
        return statusChangeView(change, { architectSessionId, now: this.now });
      },
      get statusLine() {
        return this.statusChange?.line ?? "";
      },
      get statusReason() {
        return this.statusChange?.reason ?? "";
      },

      init() {
        try {
          this.task = gateway.decodeTask(readSeed(this.$el));
          this.status = this.task.status;
        } catch (err) {
          this.taskError = describeError(err);
          return;
        }
        ticker = setInterval(() => (this.now = clock.now()), TICK_MS);
        // Moved elsewhere (the architect, another tab): the picker follows.
        this.$watch("storedStatus", (status) => this.follow(status));
      },

      destroy() {
        clearInterval(ticker);
      },

      /** The task's status changed over the feed: show it, unless this page is saving its own. */
      follow(status) {
        if (!status || !this.task || this.movingStatus || status === this.task.status) return;
        this.task = { ...this.task, status };
        this.status = status;
        this.now = clock.now();
      },

      async changeStatus() {
        if (!this.task || this.status === this.task.status) return;
        const previous = this.task.status;
        this.movingStatus = true;
        try {
          this.task = await gateway.moveTask(this.task.id, this.status);
          reload();
        } catch (err) {
          this.status = previous;
          this.taskError = describeError(err);
        } finally {
          this.movingStatus = false;
        }
      },

      startEditTask() {
        this.draftTitle = this.title;
        this.draftDescription = this.description;
        this.confirmingTaskDelete = false;
        this.editingTask = true;
        this.$nextTick(() => this.$refs.title?.focus());
      },

      cancelEditTask() {
        this.editingTask = false;
      },

      async save() {
        if (this.cannotSaveTask) return;
        this.savingTask = true;
        this.taskError = null;
        try {
          this.task = await gateway.updateTask({
            id: this.task.id,
            title: this.draftTitle.trim(),
            description: this.draftDescription.trim(),
          });
          this.editingTask = false;
        } catch (err) {
          this.taskError = describeError(err);
        } finally {
          this.savingTask = false;
        }
      },

      /** Checks the task can go (no sessions), then asks for confirmation. */
      async askDeleteTask() {
        if (!this.task || this.checkingTaskDelete) return;
        this.checkingTaskDelete = true;
        this.taskDeleteProblem = "";
        this.editingTask = false;
        try {
          this.taskDeleteProblem = deleteBlocker(await gateway.countSessions(this.task.projectId, this.task.id));
          this.confirmingTaskDelete = this.taskDeleteProblem === "";
        } catch (err) {
          this.taskError = describeError(err);
        } finally {
          this.checkingTaskDelete = false;
        }
      },

      cancelDeleteTask() {
        this.confirmingTaskDelete = false;
        this.taskDeleteProblem = "";
      },

      async removeTask() {
        if (!this.task || this.deletingTask) return;
        this.deletingTask = true;
        try {
          await gateway.deleteTask(this.task.id);
          navigate(`/projects/${encodeURIComponent(this.task.projectId)}`);
        } catch (err) {
          this.taskError = describeError(err);
          this.deletingTask = false;
        }
      },

      dismissTaskError() {
        this.taskError = null;
      },
    };
  };
