// The task page's Feature button and popover, in the title row: the task's child tasks, from the rail's
// store (every task of the project, kept live over the project feed), so a
// child an agent creates or moves while the page is open shows at once. The
// children's documents are as the server painted them (the seed); a new
// child reads "No documents yet" until a reload.
//
//   <span x-data="tasksFeature" style="display: contents" data-task-id="t1" data-project-id="p1" data-seed="feature-seed" data-children="2">
//     <button id="task-feature-button">Feature <span class="badge">2</span></button>
//     <div class="popup" id="task-feature" popover="manual">   (the page's popupDialog)
//       <div x-ignore data-ssr>…first paint…</div>
//       <template x-for="child in children"> …
//
// The popover is never remembered open; a link to #feature opens it.

import { readSeed } from "../../../../shared/presentation/seed.js";
import { childrenOf } from "../../domain/feature.js";
import { featureSummary, statusLine, toChildRows } from "../featureView.js";

/** The Feature popover: a dialog in the top layer (the page's popupDialog). */
const POPOVER_ID = "task-feature";
const BUTTON_ID = "task-feature-button";

/** @param {{ store: import("./rail.js").RailStore }} deps */
export const feature = ({ store, hash = () => globalThis.location?.hash ?? "" }) => () => ({
  taskId: "",
  projectId: "",
  /** @type {Record<string, import("../featureView.js").DocLink[]>} the painted documents by child id */
  documents: {},
  /** The live rows have rendered and the server copy is gone. */
  ready: false,
  /** The Feature popover is open (browser-only: a page loads with it closed). */
  popoverOpen: false,
  /** How many children the server painted (data-children), until the rail's store has them. */
  painted: 0,

  init() {
    this.taskId = this.$el.dataset.taskId ?? "";
    this.projectId = this.$el.dataset.projectId ?? "";
    this.painted = Number(this.$el.dataset.children ?? 0);
    try {
      const seed = this.$el.dataset.seed ? readSeed(this.$el) : null;
      this.documents = seed && typeof seed.documents === "object" && seed.documents ? seed.documents : {};
    } catch {
      this.documents = {}; // the rows still render; their documents come with a reload
    }
    if (hash() === "#feature" && this.hasChildren) {
      // The popup inside is a child component: it listens once it has started.
      this.popoverOpen = true;
      this.$nextTick(() => this.openPopover());
    }
    if (!store.seeded) return; // the server's rendering stays as it is
    this.$nextTick(() => {
      for (const node of this.$el.querySelectorAll("[data-ssr]")) node.remove();
      this.ready = true;
    });
  },

  get children() {
    if (!store.seeded) return [];
    return toChildRows(childrenOf(store.tasks, { id: this.taskId }), { projectId: this.projectId, documents: this.documents });
  },
  get hasChildren() {
    return store.seeded ? this.children.length > 0 : this.painted > 0;
  },
  get summary() {
    return featureSummary(this.children);
  },
  get count() {
    return this.summary.count;
  },
  get buttonName() {
    return this.summary.name;
  },
  /** The popover's foot: the statuses of the children in words. */
  get footLine() {
    return statusLine(this.children);
  },
  get scrollLabel() {
    return `Child tasks, ${this.children.length}`;
  },

  /** Opens the popover under the Feature button; the dialog hands the focus back to it. */
  openPopover() {
    this.popoverOpen = true;
    this.$dispatch("dialog-open", { id: POPOVER_ID, anchor: document.getElementById(BUTTON_ID) });
  },
  /** The dialog closed (Esc, Close or a press outside). */
  popoverClosed(event) {
    if (event?.detail?.id === POPOVER_ID) this.popoverOpen = false;
  },
});
