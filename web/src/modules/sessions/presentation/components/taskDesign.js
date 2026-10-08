// A task's design assets: what its sessions made (in either scope) and the
// project assets attached to it from other tasks, in two groups of one list,
// with the selected one previewed beside it as on the Design tab. From the bar
// a person detaches an attached asset, moves one the task made to the project
// or back (asking first when that detaches it elsewhere), and attaches a
// project asset from the library (a picker, loaded when opened). The list
// follows the project feed: attachments, scope moves and re-publishes of
// project assets arrive live; a new task-scope publish shows on Reload (and
// live on its session's Design tab).
//
//   <script id="task-design-seed" type="application/json">{"project_id", "ticket_id", "tasks": [{"id", "title", "href"}], "artifacts": [...]}</script>
//   <main x-data="sessionsTaskDesign" data-seed="task-design-seed"> …

import { describeError } from "../../../../shared/presentation/errors.js";
import { readSeed } from "../../../../shared/presentation/seed.js";
import { attachableAssets, belongsTo, byUpdated, relationTo, Scope } from "../../domain/artifact.js";
import { artifactTitle, kindWord, moveBackWarning } from "../artifactView.js";
import { artifactBrowsing, compose } from "./artifactBrowsing.js";
import { attachments } from "./attachments.js";
import { picker } from "../../../../shared/presentation/components/picker.js";

const NO_TASK = { title: "a deleted task", href: "" };

/**
 * @param {{
 *   artifacts: import("../../domain/ports.js").ArtifactGateway,
 *   clock: import("../../../../shared/infrastructure/clock.js").Clock,
 *   setTimeout?: typeof globalThis.setTimeout,
 * }} deps
 */
export const taskDesign = ({ artifacts, clock, setTimeout }) => () => {
  let unfollow = () => {};
  let destroyed = false;

  return compose(artifactBrowsing(clock), picker(), attachments({ artifacts, setTimeout }), {
    projectId: "",
    ticketId: "",
    /** @type {Record<string, { title: string, href: string }>} */
    tasks: {},
    ready: false,
    /** The library is being fetched for the picker. */
    loadingLibrary: false,
    /** The asset whose move back is waiting for the person to confirm the detaches. */
    movingBackId: "",

    // ── the list's rules (attachments) ───────────────────────────────────
    keeps(artifact) {
      return belongsTo(artifact, this.ticketId);
    },
    taskTitle(ticketId) {
      return (this.tasks[ticketId] ?? NO_TASK).title;
    },

    // ── what the markup binds (the rest: artifactBrowsing, picker) ───────
    cardView(artifact, card) {
      const from = this.tasks[artifact.ticketId] ?? NO_TASK;
      return { ...card, fromTitle: from.title, fromHref: from.href };
    },
    /** What the task's sessions made, newest first. */
    get madeCards() {
      return this.cards.filter((c) => c.attachedMark === "");
    },
    /** The project assets attached to it from other tasks, newest first. */
    get attachedCards() {
      return this.cards.filter((c) => c.attachedMark !== "");
    },
    get hasMade() {
      return this.madeCards.length > 0;
    },
    get hasAttached() {
      return this.attachedCards.length > 0;
    },
    get selected() {
      return this.artifacts.find((a) => a.id === this.selectedId) ?? null;
    },
    get currentIsAttached() {
      return this.selected !== null && relationTo(this.selected, this.ticketId) === "attached";
    },
    /** The task made it: the scope move is its to make. An attached asset only detaches here. */
    get canMoveCurrent() {
      return this.selected !== null && !this.currentIsAttached && (this.current?.canMove ?? false);
    },
    get currentFromTitle() {
      return this.selected ? this.taskTitle(this.selected.ticketId) : "";
    },
    get currentFromHref() {
      return this.selected ? (this.tasks[this.selected.ticketId] ?? NO_TASK).href : "";
    },
    get hasCurrentFromHref() {
      return this.currentIsAttached && this.currentFromHref !== "";
    },
    get detachAriaLabel() {
      return this.selected ? `Detach ${artifactTitle(this.selected)} from this task` : "Detach from this task";
    },
    get isEmpty() {
      return this.ready && this.artifacts.length === 0;
    },
    get countWord() {
      const n = this.artifacts.length;
      return n === 0 ? "No assets" : n === 1 ? "1 asset" : `${n} assets`;
    },
    get confirmingMoveBack() {
      return this.movingBackId !== "" && this.movingBackId === this.selectedId;
    },
    get moveBackQuestion() {
      const a = this.selected;
      if (!a) return "";
      return moveBackWarning(a, this.taskTitle(a.ticketId), a.attachedTicketIds.map((id) => this.taskTitle(id)));
    },

    // ── lifecycle ────────────────────────────────────────────────────────
    async init() {
      const seed = readSeed(this.$el);
      this.projectId = seed?.project_id ?? "";
      this.ticketId = seed?.ticket_id ?? "";
      this.tasks = Object.fromEntries((seed?.tasks ?? []).map((t) => [t.id, { title: t.title, href: t.href }]));
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
      this.selectFirst();
      this.ready = true;
      return true;
    },

    async load() {
      try {
        this.artifacts = await artifacts.listTask(this.ticketId);
        this.now = clock.now();
        this.error = null;
        if (!this.artifacts.some((a) => a.id === this.selectedId)) this.selectFirst();
      } catch (err) {
        this.error = describeError(err);
      } finally {
        this.ready = true;
      }
    },

    reload() {
      return this.load();
    },

    /** The first card as the page shows them: made here, then attached. */
    selectFirst() {
      this.selectedId = this.visibleOrder()[0] ?? "";
    },

    /** J/K walk the cards as shown: the made group, then the attached one. */
    visibleOrder() {
      return [...this.madeCards, ...this.attachedCards].map((c) => c.id);
    },
    step(delta) {
      const order = this.visibleOrder();
      if (order.length === 0) return;
      const at = order.indexOf(this.selectedId);
      const to = at === -1 ? 0 : Math.max(0, Math.min(order.length - 1, at + delta));
      this.select(order[to]);
    },

    // ── person actions ───────────────────────────────────────────────────
    /** Picks a project asset this task does not have yet; the library is fetched now, so it is current. */
    async openAssetPicker() {
      if (this.loadingLibrary) return;
      this.loadingLibrary = true;
      this.error = null;
      let library;
      try {
        library = await artifacts.listProject(this.projectId);
      } catch (err) {
        this.error = describeError(err);
        return;
      } finally {
        this.loadingLibrary = false;
      }
      this.openPicker({
        label: "Attach a project asset to this task",
        emptyText: "This task already has every project asset. Move one to the project from a session's Design tab first.",
        choices: attachableAssets(library, this.ticketId).map((a) => ({
          id: a.id,
          label: artifactTitle(a),
          detail: `${kindWord(a.kind)} · from ${this.taskTitle(a.ticketId)}`,
        })),
        onPick: (id) => this.attachAsset(id),
      });
    },

    async attachAsset(artifactId) {
      if (await this.attachTo(artifactId, this.ticketId)) this.select(artifactId);
    },

    detachCurrent() {
      const target = this.selected;
      if (target && this.currentIsAttached) return this.detachFrom(target.id, this.ticketId);
    },

    /** Moves an asset the task made to the project, or back (asking first when it is attached elsewhere). */
    async move() {
      const target = this.selected;
      if (!target || !this.canMoveCurrent || this.isPending(target.id)) return;
      if (target.scope === Scope.PROJECT && target.attachedTicketIds.length > 0 && this.movingBackId !== target.id) {
        this.movingBackId = target.id;
        return;
      }
      this.movingBackId = "";
      await this.act(target.id, () => artifacts.setScope(target.id, target.scope === Scope.PROJECT ? Scope.TASK : Scope.PROJECT));
    },
    confirmMoveBack() {
      return this.move();
    },
    cancelMoveBack() {
      this.movingBackId = "";
    },
  });
};
