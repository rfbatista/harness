// The project's design assets: the artifacts moved to project level, newest
// first, each naming the task it came from and the tasks it is attached to,
// with the selected one previewed beside the list as on the Design tab. From
// the bar a person attaches it to another task (a picker of the project's
// tasks), detaches it from one, moves it back to its task (asking first when
// that would detach it; it leaves the list and a banner links the task) or
// deletes it, after a confirmation. The list follows the project feed, so
// what agents and other people do shows without a reload.
//
//   <script id="design-library-seed" type="application/json">{"project_id": "p1", "tasks": [{"id", "title", "href"}], "artifacts": [...]}</script>
//   <section x-data="sessionsDesignLibrary" data-seed="design-library-seed"> …

import { describeError } from "../../../../shared/presentation/errors.js";
import { readSeed } from "../../../../shared/presentation/seed.js";
import { attachableTasks, byUpdated, Scope } from "../../domain/artifact.js";
import { artifactTitle, moveBackWarning } from "../artifactView.js";
import { artifactBrowsing, compose } from "./artifactBrowsing.js";
import { attachments } from "./attachments.js";
import { picker } from "./picker.js";

const NO_TASK = { title: "a deleted task", href: "" };

/**
 * @param {{
 *   artifacts: import("../../domain/ports.js").ArtifactGateway,
 *   clock: import("../../../../shared/infrastructure/clock.js").Clock,
 *   setTimeout?: typeof globalThis.setTimeout,
 * }} deps
 */
export const designLibrary = ({ artifacts, clock, setTimeout }) => () => {
  let unfollow = () => {};
  let destroyed = false;

  return compose(artifactBrowsing(clock), picker(), attachments({ artifacts, setTimeout }), {
    projectId: "",
    /** @type {Record<string, { title: string, href: string }>} */
    tasks: {},
    /** The seed's order, for the picker. */
    taskOrder: [],
    ready: false,
    deletingId: "",
    /** The asset whose move back is waiting for the person to confirm the detaches. */
    movingBackId: "",
    /** @type {{ title: string, taskTitle: string, taskHref: string } | null} */
    movedBack: null,

    // ── the list's rules (attachments) ───────────────────────────────────
    keeps(artifact) {
      return artifact.scope === Scope.PROJECT && artifact.projectId === this.projectId;
    },
    taskTitle(ticketId) {
      return this.taskOf({ ticketId }).title;
    },

    // ── what the markup binds (the rest: artifactBrowsing, picker) ───────
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
    /** The tasks the selected asset is attached to, each with its detach button's words. */
    get attachedTasks() {
      const a = this.selected;
      if (!a) return [];
      const title = artifactTitle(a);
      return a.attachedTicketIds.map((id) => {
        const task = this.taskOf({ ticketId: id });
        return { id, title: task.title, href: task.href, linked: task.href !== "", unlinked: task.href === "", detachLabel: `Detach ${title} from ${task.title}` };
      });
    },
    get hasAttachedTasks() {
      return this.attachedTasks.length > 0;
    },
    get attachAriaLabel() {
      return this.selected ? `Attach ${artifactTitle(this.selected)} to a task` : "Attach to a task";
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
    get deleteConsequence() {
      const n = this.selected?.attachedTicketIds.length ?? 0;
      const also = n === 0 ? "" : n === 1 ? ", and from the task it is attached to" : `, and from the ${n} tasks it is attached to`;
      return `It is removed for good, from the project and from its task${also}.`;
    },
    get deleteAriaLabel() {
      return this.selected ? `Delete ${artifactTitle(this.selected)}` : "Delete";
    },
    get moveBackAriaLabel() {
      return this.selected ? `Move ${artifactTitle(this.selected)} back to task` : "Move back to task";
    },
    get confirmingMoveBack() {
      return this.movingBackId !== "" && this.movingBackId === this.selectedId;
    },
    get moveBackQuestion() {
      const a = this.selected;
      if (!a) return "";
      return moveBackWarning(a, this.taskOf(a).title, a.attachedTicketIds.map((id) => this.taskTitle(id)));
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
      const tasks = seed?.tasks ?? [];
      this.tasks = Object.fromEntries(tasks.map((t) => [t.id, { title: t.title, href: t.href }]));
      this.taskOrder = tasks.map((t) => t.id);
      if (!this.hydrate(seed?.artifacts)) await this.load();
      if (destroyed || !this.projectId) return;
      unfollow = artifacts.followProject(
        this.projectId,
        (change) => this.onProjectChange(change),
        (status) => this.onFeedStatus(status),
      );
    },

    destroy() {
      destroyed = true;
      unfollow();
    },

    /** The server's first paint: the seeded rows, when it sent them and they read. */
    hydrate(rows) {
      if (!Array.isArray(rows)) return false;
      try {
        this.artifacts = byUpdated(artifacts.decodeArtifacts(rows)).filter((a) => this.keeps(a));
      } catch {
        return false;
      }
      this.now = clock.now();
      this.keepSelection();
      this.ready = true;
      return true;
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
    /** Picks a task of the project to attach the selected asset to. */
    openAttachPicker() {
      const target = this.selected;
      if (!target) return;
      const tasks = this.taskOrder.map((id) => ({ id, ...this.tasks[id] }));
      this.openPicker({
        label: `Attach ${artifactTitle(target)} to a task`,
        emptyText: "Every task of this project already has this asset.",
        choices: attachableTasks(target, tasks).map((t) => ({ id: t.id, label: t.title })),
        onPick: (ticketId) => this.attachTo(target.id, ticketId),
      });
    },

    /** @param {string} ticketId */
    detachTask(ticketId) {
      const target = this.selected;
      if (target) return this.detachFrom(target.id, ticketId);
    },

    /** Back to its task: asks first when that detaches it from other tasks. */
    async moveBack() {
      const target = this.selected;
      if (!target || this.isPending(target.id)) return;
      if (target.attachedTicketIds.length > 0 && this.movingBackId !== target.id) {
        this.movingBackId = target.id;
        return;
      }
      await this.confirmMoveBack();
    },
    async confirmMoveBack() {
      const target = this.selected;
      this.movingBackId = "";
      if (!target) return;
      const task = this.taskOf(target);
      const moved = await this.act(target.id, () => artifacts.setScope(target.id, Scope.TASK));
      if (moved) this.movedBack = { title: artifactTitle(target), taskTitle: task.title, taskHref: task.href };
    },
    cancelMoveBack() {
      this.movingBackId = "";
    },

    askDelete() {
      this.deletingId = this.selectedId;
    },
    cancelDelete() {
      this.deletingId = "";
    },
    async confirmDelete() {
      const id = this.deletingId;
      if (!id) return;
      const removed = await this.act(id, () => artifacts.remove(id));
      if (removed) this.applyChange({ kind: "deleted", id, projectId: this.projectId, ticketId: "", attachedTicketIds: [] });
      this.deletingId = "";
    },

    dismissMovedBack() {
      this.movedBack = null;
    },
  });
};
