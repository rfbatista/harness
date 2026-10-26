// What every catalog editor shares around its form: the toast after a save
// (the server's summary, Undo, and what running sessions keep), the Review
// change panel, and the warning before leaving with unsaved changes.
//
// The editor it is mixed into provides `sv` (domain/savePipeline.js saveView)
// and `closeEditor()`. Markup: agents/save.templ.

import { describeRefusal } from "../../domain/refusals.js";
import { refusedUndoToast, undoneToast } from "../../domain/savePipeline.js";

/** @param {{ gateway: Pick<import("../../domain/ports.js").CatalogGateway, "undoChange"> }} deps */
export function saveUi({ gateway }) {
  return {
    /** @type {{ text: string, tone: string, note: string, undoId: string, hasUndo: boolean, noUndo: boolean, hasNote: boolean } | null} */
    toast: null,
    /** Where the person was going when the warning stopped them: { go }. */
    leaving: null,
    reviewOpen: false,
    /** The page is going to another form with the edit kept: the browser's prompt would be wrong. */
    departing: false,

    /** Whether leaving would lose unsaved changes. */
    get leaveGuarded() {
      return this.sv.dirty && !this.departing;
    },
    get notLeaving() {
      return this.leaving === null;
    },
    get reviewLabel() {
      return this.reviewOpen ? "Hide change" : "Review change";
    },
    get reviewShown() {
      return this.reviewOpen && this.sv.review;
    },

    /** @param {{ text: string, tone: string, note: string, undoId: string }} t */
    showToast(t) {
      this.toast = { ...t, hasUndo: t.undoId !== "", noUndo: t.undoId === "", hasNote: t.note !== "" };
    },
    dismissToast() {
      this.toast = null;
    },

    /** Undo the change the toast reports. A refusal replaces the toast with its reason. */
    async undoLast() {
      const id = this.toast?.undoId;
      if (!id) return;
      try {
        const result = await gateway.undoChange(id);
        this.showToast(undoneToast(result.change.summary));
        await this.load();
      } catch (err) {
        this.showToast(refusedUndoToast(describeRefusal(err, { undo: true })));
      }
    },

    /** The leave warning appeared: put the keyboard on its safe choice. @param {HTMLElement} el */
    focusKeep(el) {
      el.querySelector("[data-keep]")?.focus();
    },

    toggleReview() {
      this.reviewOpen = !this.reviewOpen;
      if (this.reviewOpen) this.$nextTick?.(() => document.getElementById("review-panel")?.scrollIntoView?.({ block: "nearest" }));
    },

    /** Runs `go` now, or after the person confirms when it would lose unsaved changes. */
    guarded(go) {
      if (this.leaveGuarded) this.leaving = { go };
      else go();
    },
    keepEditing() {
      this.leaving = null;
    },
    discardAndLeave() {
      const l = this.leaving;
      this.leaving = null;
      // A leave that is not a navigation (the MCP drawer) closes only itself: the editor under it stays.
      if (!l?.inPlace) this.closeEditor();
      l?.go();
    },

    /** The browser's own prompt: closing the window or following a link away. */
    beforeUnload(event) {
      if (!this.leaveGuarded) return;
      event.preventDefault();
      event.returnValue = "";
    },
  };
}
