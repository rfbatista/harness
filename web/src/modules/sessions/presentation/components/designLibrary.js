// The project's design assets: the artifacts moved to project level, newest
// first, each naming the task it came from, with the selected one previewed
// beside the list as on the Design tab. From the bar a person moves it back
// to its task (it leaves the list; a banner links the task) or deletes it,
// after a confirmation. The page loads on open and on its Reload button: scope
// changes are announced on each session's stream, not on a project one.
//
//   <script id="design-library-seed" type="application/json">{"project_id": "p1", "tasks": [{"id", "title", "href"}]}</script>
//   <section x-data="sessionsDesignLibrary" data-seed="design-library-seed"> …

import { describeError } from "../../../../shared/presentation/errors.js";
import { readSeed } from "../../../../shared/presentation/seed.js";
import { Scope, withoutArtifact } from "../../domain/artifact.js";
import { artifactTitle } from "../artifactView.js";
import { artifactBrowsing, compose } from "./artifactBrowsing.js";

const NO_TASK = { title: "a deleted task", href: "" };

/**
 * @param {{
 *   artifacts: import("../../domain/ports.js").ArtifactGateway,
 *   clock: import("../../../../shared/infrastructure/clock.js").Clock,
 * }} deps
 */
export const designLibrary = ({ artifacts, clock }) => () =>
  compose(artifactBrowsing(clock), {
    projectId: "",
    /** @type {Record<string, { title: string, href: string }>} */
    tasks: {},
    ready: false,
    error: null,
    busy: false,
    deletingId: "",
    /** @type {{ title: string, taskTitle: string, taskHref: string } | null} */
    movedBack: null,

    // ── what the markup binds (the rest: artifactBrowsing) ───────────────
    taskOf(artifact) {
      return this.tasks[artifact.ticketId] ?? NO_TASK;
    },
    cardView(artifact, card) {
      return { ...card, taskTitle: this.taskOf(artifact).title };
    },
    get selected() {
      return this.artifacts.find((a) => a.id === this.selectedId) ?? null;
    },
    get currentTaskTitle() {
      return this.selected ? this.taskOf(this.selected).title : "";
    },
    get currentTaskHref() {
      return this.selected ? this.taskOf(this.selected).href : "";
    },
    get hasCurrentTaskHref() {
      return this.currentTaskHref !== "";
    },
    get isEmpty() {
      return this.ready && this.artifacts.length === 0;
    },
    get countWord() {
      const n = this.artifacts.length;
      return n === 0 ? "No assets" : n === 1 ? "1 asset" : `${n} assets`;
    },
    get confirmingDelete() {
      return this.deletingId !== "" && this.deletingId === this.selectedId;
    },
    get deleteQuestion() {
      return this.selected ? `Delete ${artifactTitle(this.selected)}?` : "";
    },
    get deleteAriaLabel() {
      return this.selected ? `Delete ${artifactTitle(this.selected)}` : "Delete";
    },
    get moveBackAriaLabel() {
      return this.selected ? `Move ${artifactTitle(this.selected)} back to task` : "Move back to task";
    },
    get hasMovedBack() {
      return this.movedBack !== null;
    },
    get movedBackTitle() {
      return this.movedBack?.title ?? "";
    },
    get movedBackTaskTitle() {
      return this.movedBack?.taskTitle ?? "";
    },
    get movedBackHref() {
      return this.movedBack?.taskHref ?? "";
    },
    /** The CSP build cannot negate in markup: one getter per branch. */
    get movedBackLinks() {
      return this.movedBackHref !== "";
    },
    get movedBackUnlinked() {
      return this.hasMovedBack && this.movedBackHref === "";
    },

    // ── lifecycle ────────────────────────────────────────────────────────
    async init() {
      const seed = readSeed(this.$el);
      this.projectId = seed?.project_id ?? "";
      this.tasks = Object.fromEntries((seed?.tasks ?? []).map((t) => [t.id, { title: t.title, href: t.href }]));
      await this.load();
    },

    async load() {
      try {
        this.artifacts = await artifacts.listProject(this.projectId);
        this.now = clock.now();
        this.error = null;
        this.keepSelection();
      } catch (err) {
        this.error = describeError(err);
      } finally {
        this.ready = true;
      }
    },

    reload() {
      return this.load();
    },

    // ── person actions ───────────────────────────────────────────────────
    /** Back to its task: it leaves the library, and the banner links the task. */
    async moveBack() {
      const target = this.selected;
      if (this.busy || !target) return;
      await this.act(async () => {
        await artifacts.setScope(target.id, Scope.TASK);
        const task = this.taskOf(target);
        this.drop(target.id);
        this.movedBack = { title: artifactTitle(target), taskTitle: task.title, taskHref: task.href };
      });
    },

    askDelete() {
      this.deletingId = this.selectedId;
    },
    cancelDelete() {
      this.deletingId = "";
    },
    async confirmDelete() {
      const id = this.deletingId;
      if (this.busy || !id) return;
      await this.act(async () => {
        await artifacts.remove(id);
        this.drop(id);
      });
      this.deletingId = "";
    },

    async act(fn) {
      this.busy = true;
      this.error = null;
      try {
        await fn();
      } catch (err) {
        this.error = describeError(err);
      } finally {
        this.busy = false;
      }
    },

    /** Takes an asset off the list; the next one (or the one before) opens. */
    drop(id) {
      const at = this.artifacts.findIndex((a) => a.id === id);
      this.artifacts = withoutArtifact(this.artifacts, id);
      this.selectedId = this.artifacts[Math.min(at, this.artifacts.length - 1)]?.id ?? "";
    },

    dismissMovedBack() {
      this.movedBack = null;
    },
    dismissError() {
      this.error = null;
    },
  });
