// Moves the open document between task and project scope from a documents
// page, in place: the scope word, the list's mark and the button's label
// follow the answer; the document stays in the list. The page's document
// watch sees the new updated_at on its next poll.
//
//   <div x-data="tasksDocumentScope" data-document-id="d1" data-scope="task" data-title="Plan">
//     <span x-text="word">Task document</span>
//     <form x-on:submit.prevent="move"><button type="submit" x-text="label" x-bind:aria-label="ariaLabel">Move to project</button></form>
//   </div>

import { describeError } from "../../../../shared/presentation/errors.js";

/** @param {{ gateway: import("../../domain/ports.js").TaskGateway }} deps */
export const documentScope = ({ gateway }) => () => ({
  documentId: "",
  title: "",
  scope: "task",
  moving: false,
  error: null,

  get isProject() {
    return this.scope === "project";
  },
  /** The scope in words, never by colour alone. */
  get word() {
    return this.isProject ? "Project document" : "Task document";
  },
  /** The list's mark on a project document; nothing on a task document. */
  get mark() {
    return this.isProject ? "project" : "";
  },
  /** The scope a move would give the document. */
  get target() {
    return this.isProject ? "task" : "project";
  },
  get label() {
    return this.isProject ? "Move back to task" : "Move to project";
  },
  /** The button's accessible name: what it does, to which document. */
  get ariaLabel() {
    return this.isProject ? `Move ${this.title} back to task` : `Move ${this.title} to project`;
  },

  init() {
    const { documentId = "", scope = "task", title = "" } = this.$el.dataset;
    this.documentId = documentId;
    this.scope = scope;
    this.title = title;
  },

  async move() {
    if (this.moving || !this.documentId) return;
    this.moving = true;
    this.error = null;
    try {
      const moved = await gateway.setDocumentScope(this.documentId, this.target);
      this.scope = moved.scope;
    } catch (err) {
      this.error = describeError(err);
    } finally {
      this.moving = false;
    }
  },

  dismissError() {
    this.error = null;
  },
});
