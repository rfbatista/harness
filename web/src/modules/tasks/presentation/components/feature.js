// The task page's feature section: the task's child tasks, from the rail's
// store (every task of the project, kept live over the project feed), so a
// child an agent creates or moves while the page is open shows at once. The
// children's documents are as the server painted them (the seed); a new
// child reads "No documents yet" until a reload.
//
//   <section x-data="tasksFeature" data-task-id="t1" data-project-id="p1" data-seed="feature-seed" x-show="hasChildren">
//     <div x-ignore data-ssr>…first paint…</div>
//     <template x-for="child in children"> …

import { readSeed } from "../../../../shared/presentation/seed.js";
import { childrenOf } from "../../domain/feature.js";
import { toChildRows } from "../featureView.js";

/** @param {{ store: import("./rail.js").RailStore }} deps */
export const feature = ({ store }) => () => ({
  taskId: "",
  projectId: "",
  /** @type {Record<string, import("../featureView.js").DocLink[]>} the painted documents by child id */
  documents: {},
  /** The live rows have rendered and the server copy is gone. */
  ready: false,

  init() {
    this.taskId = this.$el.dataset.taskId ?? "";
    this.projectId = this.$el.dataset.projectId ?? "";
    try {
      const seed = this.$el.dataset.seed ? readSeed(this.$el) : null;
      this.documents = seed && typeof seed.documents === "object" && seed.documents ? seed.documents : {};
    } catch {
      this.documents = {}; // the rows still render; their documents come with a reload
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
    return store.seeded ? this.children.length > 0 : this.$el.style.display !== "none";
  },
  get count() {
    return String(this.children.length);
  },
});
