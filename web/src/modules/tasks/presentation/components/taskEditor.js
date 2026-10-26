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

/** The preference that keeps what the person chose for a task's description: "open" or "closed"; absent is no choice. */
const descriptionKey = (taskId) => `task.description.${taskId}`;

/**
 * @param {{
 *   gateway: import("../../domain/ports.js").TaskGateway,
 *   navigate: (url: string) => void,
 *   reload: () => void,
 *   store?: import("./rail.js").RailStore,  the rail's live model: the task as the feed has it, and its latest move
 *   clock?: { now: () => Date },
 *   prefs?: import("../../../../shared/infrastructure/storage.js").Preferences,  remembers per task whether the description shows whole
 * }} deps
 */
export const taskEditor =
  ({ gateway, navigate, reload, store = { tasks: [], statusChanges: {} }, clock = { now: () => new Date() }, prefs = { get: () => null, set: () => {} } }) =>
  () => {
    let ticker = null;
    let onResize = () => {};
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
      /** Archive or Restore is on its way. */
      archiving: false,
      /** What the person chose for this task's description: true whole, false one line, null nothing yet. */
      descriptionChoice: null,
      /** A session is open on the page: the code needs the room, so the description is one line unless chosen otherwise. */
      sessionOpen: true,
      /** The collapsed description is wider than its line: there is more to show. */
      descriptionOverflows: false,

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
      /** The description shows whole, not cut to one line: the choice, else whole while no session is open. */
      get descriptionOpen() {
        return this.descriptionChoice ?? !this.sessionOpen;
      },
      get descriptionToggleWord() {
        return this.descriptionOpen ? "Less" : "More";
      },
      /** What x-bind sets on the note: present while the description shows whole. */
      get descriptionOpenAttr() {
        return this.descriptionOpen ? "" : null;
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
      get isArchived() {
        return Boolean(this.task?.archivedAt);
      },
      /** Only a done task on the board can be archived. */
      get canArchive() {
        return this.task?.status === "done" && !this.isArchived;
      },
      /** "Archived Oct 18.": the archived banner's lead. */
      get archivedWhen() {
        if (!this.isArchived) return "";
        const d = new Date(this.task.archivedAt);
        return `Archived ${d.toLocaleDateString("en-US", { month: "short", day: "numeric" })}.`;
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
          const stored = prefs.get(descriptionKey(this.task.id));
          this.descriptionChoice = stored === "open" ? true : stored === "closed" ? false : null;
          // It was open because it overflowed; an open one is not measured, and keeps its Less.
          this.descriptionOverflows = this.descriptionChoice === true;
        } catch (err) {
          this.taskError = describeError(err);
          return;
        }
        ticker = setInterval(() => (this.now = clock.now()), TICK_MS);
        onResize = () => this.measureDescription();
        window.addEventListener("resize", onResize);
        // Moved elsewhere (the architect, another tab): the picker follows.
        this.$watch("storedStatus", (status) => this.follow(status));
      },

      destroy() {
        clearInterval(ticker);
        window.removeEventListener("resize", onResize);
      },

      /**
       * Whether the collapsed description is wider than its one line. Measured
       * while it is collapsed (and again on a resize, which can make it fit or
       * not); an open one keeps its toggle so it can be closed.
       */
      measureDescription() {
        const open = this.descriptionOpen; // read now, so the effect that calls this follows it
        this.$nextTick(() => {
          const text = this.$refs.description;
          if (!text || open) return;
          this.descriptionOverflows = text.scrollWidth > text.clientWidth + 1;
        });
      },

      /** The page says whether a session is open (x-effect on the description). */
      setSessionOpen(open) {
        this.sessionOpen = open === true;
      },

      toggleDescription() {
        this.descriptionChoice = !this.descriptionOpen;
        if (this.task) prefs.set(descriptionKey(this.task.id), this.descriptionChoice ? "open" : "closed");
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

      /** Takes the done task off the board; the page reloads to show it archived. */
      archiveTask() {
        return this.setArchived(() => gateway.archiveTask(this.task.id));
      },

      /** Puts the archived task back in Done, on the board. */
      restoreTask() {
        return this.setArchived(() => gateway.restoreTask(this.task.id));
      },

      async setArchived(send) {
        if (!this.task || this.archiving) return;
        this.archiving = true;
        this.taskError = null;
        try {
          this.task = await send();
          reload();
        } catch (err) {
          this.taskError = describeError(err);
        } finally {
          this.archiving = false;
        }
      },

      startEditTask() {
        this.draftTitle = this.title;
        this.draftDescription = this.description;
        this.confirmingTaskDelete = false;
        this.editingTask = true;
        // By id, not x-ref: the title sits inside the writing helper's own x-data.
        this.$nextTick(() => document.getElementById("task-edit-title")?.focus());
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
