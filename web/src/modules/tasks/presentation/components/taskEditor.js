// The task in the task page's header: its title, status and description,
// edited in the task note popover, and deleted. Each edit sends only what changed: a status
// change is status-only and reloads the page, so the rail regroups the task;
// a text edit does not resend the status. A task with sessions is not deleted (they would be left
// under no task); the editor asks the server right before deleting it.
//
//   <div x-data="tasksTaskEditor" data-seed="task-seed" data-savers-seed="task-savers-seed"> … </div>
//
// The edit form's third section is the task's Token savers: the choices are
// held in the form (savers[].draft) and written on Save, one activation per
// changed saver, after the task itself; Cancel drops them. The fourth is the
// task's trajectory recording (inherit / on / off), held and written the same
// way, after the savers.

import { describeError } from "../../../../shared/presentation/errors.js";
import { readSeed } from "../../../../shared/presentation/seed.js";
import { choiceOf, resolution } from "../../domain/recording.js";
import { deleteBlocker, titleProblem } from "../../domain/task.js";
import { statusChangeView } from "../statusChange.js";

const TICK_MS = 30_000;

/** The task note: a popover in the top layer (the page's popupDialog) with the description and the Edit form. Its open state is never stored. */
const NOTE_ID = "task-note";

/**
 * @param {{
 *   gateway: import("../../domain/ports.js").TaskGateway,
 *   navigate: (url: string) => void,
 *   reload: () => void,
 *   tokenSavers?: import("../../../tokensavers/domain/ports.js").TokenSaverGateway,  the task's Token savers, written on Save
 *   recording?: ReturnType<typeof import("../../infrastructure/recording-gateway.js").recordingGateway>,  the task's trajectory recording, written on Save
 *   store?: import("./railFeed.js").RailStore,  the rail's live model: the task as the feed has it, and its latest move
 *   clock?: { now: () => Date },
 * }} deps
 */
export const taskEditor =
  ({ gateway, tokenSavers, recording, navigate, reload, store = { tasks: [], statusChanges: {} }, clock = { now: () => new Date() } }) =>
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
      /** Archive or Restore is on its way. */
      archiving: false,
      /**
       * The task's Token savers as the Edit form holds them: value is what is
       * saved, draft what the person chose, summary the server's line on what it resolves to.
       * @type {{ saver: string, value: string, draft: string, summary: string, inputId: string }[]}
       */
      savers: [],
      /** The owner the savers are written to (the task, at task level). */
      saversLevel: "",
      saversOwner: "",
      /** A saver write that failed: the form stays open and shows it. */
      saversError: null,
      /** The task's trajectory recording as the server has it; null when the page does not carry it. */
      recordingState: null,
      /** The choice the form holds: "inherit", "on" or "off". */
      recordingDraft: "inherit",
      /** A recording write that failed: the form stays open and shows it. */
      recordingError: null,
      /** The task note popover is open (browser-only, never stored: a page loads with it closed). */
      noteOpen: false,

      get title() {
        return this.task?.title ?? "";
      },
      /** The task has a Token savers control to show. */
      get hasSavers() {
        return this.savers.length > 0;
      },

      /** The task has a Trajectory recording control to show. */
      get hasRecording() {
        return this.recordingState !== null;
      },

      /** What is saved: "inherit", "on" or "off". */
      get recordingValue() {
        return this.recordingState ? choiceOf(this.recordingState.explicit) : "inherit";
      },

      /** What would hold with the form's choice, and from where. */
      get recordingSummary() {
        return this.recordingState ? resolution(this.recordingState, this.recordingDraft) : "";
      },
      get description() {
        return this.task?.description ?? "";
      },
      get hasDescription() {
        return this.description.trim() !== "";
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
      /** The title and description as the feed has them: what the note follows. */
      get storedText() {
        const t = this.storedTask;
        return t ? JSON.stringify([t.title, t.description ?? ""]) : "";
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
        } catch (err) {
          this.taskError = describeError(err);
          return;
        }
        this.readSavers();
        this.readRecording();
        ticker = setInterval(() => (this.now = clock.now()), TICK_MS);
        // Moved elsewhere (the architect, another tab): the picker follows.
        this.$watch("storedStatus", (status) => this.follow(status));
        this.$watch("storedText", (text) => this.followText(text));
      },

      destroy() {
        clearInterval(ticker);
      },

      /** The savers the server painted, if it serves them and the page carries them. */
      readSavers() {
        const id = this.$el.dataset.saversSeed;
        const script = id ? this.$el.ownerDocument.getElementById(id) : null;
        if (!script || !tokenSavers) return;
        try {
          this.applySavers(tokenSavers.decodeActivations(JSON.parse(script.textContent ?? "null")));
        } catch (err) {
          this.saversError = describeError(err);
        }
      },

      /** The recording the server painted, if it serves it and the page carries it. */
      readRecording() {
        const id = this.$el.dataset.recordingSeed;
        const script = id ? this.$el.ownerDocument.getElementById(id) : null;
        if (!script || !recording) return;
        try {
          this.recordingState = recording.decode(JSON.parse(script.textContent ?? "null"));
          this.recordingDraft = this.recordingValue;
        } catch (err) {
          this.recordingError = describeError(err);
        }
      },

      /** @param {import("../../../tokensavers/domain/ports.js").Activations} a  as the server now has them */
      applySavers(a) {
        this.saversLevel = a.level;
        this.saversOwner = a.ownerId;
        this.savers = a.activations.map((x) => ({ saver: x.saver, value: x.value, draft: x.value, summary: x.summary, inputId: `task-saver-${x.saver}` }));
      },

      /** Back to what is saved: every choice dropped, no error. */
      resetSavers() {
        for (const s of this.savers) s.draft = s.value;
        this.saversError = null;
        this.recordingDraft = this.recordingValue;
        this.recordingError = null;
      },

      /** Opens the note under the Description button; the dialog hands the focus back to it. */
      openNote() {
        this.noteOpen = true;
        this.$dispatch("dialog-open", { id: NOTE_ID, anchor: document.getElementById("task-note-button") ?? document.getElementById("task-edit-button") });
      },

      /** The dialog closed (Esc, Close, a press outside, or Save): the note is read-only the next time. */
      noteClosed(event) {
        if (event?.detail?.id !== NOTE_ID) return;
        this.noteOpen = false;
        this.editingTask = false;
      },

      closeNote() {
        this.$dispatch("dialog-dismiss", { id: NOTE_ID });
      },

      /** A text change seen over the feed: the note shows it, unless the person is editing or saving. */
      followText() {
        const stored = this.storedTask;
        if (!stored || !this.task || this.editingTask || this.savingTask) return;
        if (stored.title === this.task.title && (stored.description ?? "") === (this.task.description ?? "")) return;
        this.task = { ...this.task, title: stored.title, description: stored.description ?? "" };
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
        this.resetSavers();
        this.editingTask = true;
        if (!this.noteOpen) this.openNote();
        // By id, not x-ref: the title sits inside the writing helper's own x-data.
        this.$nextTick(() => document.getElementById("task-edit-title")?.focus());
      },

      /** Back to the read view; a task with no description has none, so the surface closes. */
      cancelEditTask() {
        this.resetSavers();
        this.editingTask = false;
        if (!this.hasDescription) this.closeNote();
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
        } catch (err) {
          this.taskError = describeError(err);
          this.savingTask = false;
          return;
        }
        try {
          if ((await this.saveSavers()) && (await this.saveRecording())) {
            this.editingTask = false;
            this.closeNote();
          }
        } finally {
          this.savingTask = false;
        }
      },

      /**
       * One activation per saver the person changed; the first refusal stops
       * the rest and keeps the form open. The others keep the person's choice.
       * @returns {Promise<boolean>} every change landed
       */
      async saveSavers() {
        for (const row of this.savers.filter((s) => s.draft !== s.value)) {
          try {
            const answer = await tokenSavers.setActivation(this.saversLevel, this.saversOwner, row.saver, row.draft);
            for (const x of answer.activations) {
              const r = this.savers.find((s) => s.saver === x.saver);
              if (!r) continue;
              if (r === row) r.draft = x.value;
              r.value = x.value;
              r.summary = x.summary;
            }
          } catch (err) {
            this.saversError = describeError(err);
            return false;
          }
        }
        return true;
      },

      /**
       * The recording choice, when the person changed it; a refusal keeps the
       * form open with the person's choice.
       * @returns {Promise<boolean>} nothing to write, or it landed
       */
      async saveRecording() {
        if (!this.recordingState || this.recordingDraft === this.recordingValue) return true;
        try {
          this.recordingState = await recording.set(this.task.projectId, this.task.id, this.recordingDraft);
          this.recordingDraft = this.recordingValue;
          return true;
        } catch (err) {
          this.recordingError = describeError(err);
          return false;
        }
      },

      /** An item of the task menu (the title row's ⋯): Delete task asks for confirmation; the links go where they point. */
      taskMenuSelect(event) {
        if (event.detail?.action === "delete-task") return this.askDeleteTask();
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
