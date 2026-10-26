// The delete dialog of the Agents page: Delete in a detail pane's menu opens it
// (agents/delete.templ), it lists what the item reaches, and Delete turns on
// when the person has typed the name. The write runs through the save
// pipeline (saveController): the versions the page read go with it, so an
// outside edit shows the conflict banner with Reload and Overwrite, and the
// shared toast then offers Undo. Mixed into the Agents page, which provides
// the catalog, the route (tab, item), `load()`, `show()` and the toast.
//
// The dialog is a div with role alertdialog, not a native modal <dialog>: the
// focus is kept inside it here (Tab wraps, Escape closes), and it gives the
// focus back to the menu button that opened it.

import { deleteBaseVersions, deleteImpact } from "../../domain/deleteImpact.js";
import { openSave, saveView, savedToast } from "../../domain/savePipeline.js";
import { saveController } from "./saveController.js";

const KIND_OF_TAB = { agents: "agent", skills: "skill", bundles: "bundle", mcp: "mcp" };
const VERB_OF_KIND = { agent: "deleteAgent", skill: "deleteSkill", bundle: "deleteBundle", mcp: "deleteMcpServer" };
const WORD_OF_KIND = { agent: "agent", skill: "skill", bundle: "bundle", mcp: "MCP server" };
const FOCUSABLE = "input:not([disabled]), button:not([disabled]), a[href]";

/**
 * @param {{
 *   gateway: Pick<import("../../domain/ports.js").CatalogGateway, "deleteAgent" | "deleteSkill" | "deleteBundle" | "deleteMcpServer">,
 *   timers?: { setTimeout: (fn: () => void, ms: number) => any, clearTimeout: (id: any) => void },
 * }} deps
 */
export function deleteUi({ gateway, timers }) {
  /** @type {any} */
  let page;
  /** @type {ReturnType<typeof saveController> | null} */
  let saver = null;

  return {
    /** The open dialog (domain/deleteImpact.js), or null. */
    del: null,
    /** What the person typed to confirm. */
    delTyped: "",
    delState: openSave({}),
    /** The menu button that opened the dialog, which gets the focus back. */
    delReturn: null,

    get delView() {
      return saveView(this.delState, { valid: true, changes: 1, writes: [] });
    },
    get delBusy() {
      return this.delState.saving;
    },
    /** Delete is on once the name is typed exactly, and nothing is in flight or waiting on a choice. */
    get delReady() {
      return this.del !== null && !this.isBroken && this.delTyped === this.del.typed && !this.delState.saving && this.delState.phase !== "conflict";
    },
    /** Deleting is off while the catalog does not parse. */
    get delBlockedNote() {
      return this.isBroken ? `Deleting is off · ${this.brokenWhy}. Fix the file by hand, then Check again.` : "";
    },
    get delNotReady() {
      return !this.delReady;
    },
    get delButtonLabel() {
      if (this.delState.saving) return "Deleting…";
      return this.del?.button ?? "Delete";
    },
    get delNothing() {
      return this.del?.reachesNothing ?? false;
    },
    get delHasTotal() {
      return (this.del?.total ?? "") !== "";
    },
    get delConflictFiles() {
      return this.delView.conflictFiles;
    },
    get delConflictTitle() {
      const paths = (this.delState.conflict?.changed ?? []).map((c) => c.path).join(", ");
      return `${paths || "A file"} changed on disk since this page read it.`;
    },
    get delConflictNote() {
      return "Reload shows the catalog as it is now, with what the item reaches now. Delete anyway removes it regardless, so what changed on disk goes with it. There is no merge.";
    },
    /** A refusal that is not the conflict. */
    get delHasError() {
      return this.delState.refusal !== null;
    },
    get delErrorText() {
      return this.delState.refusal?.message ?? "";
    },
    get delErrorCode() {
      return this.delState.refusal?.code ?? "";
    },
    get delErrorNext() {
      return this.delState.refusal?.next ?? "";
    },

    /** The menu's Delete: opens the dialog for the open item. @param {{ currentTarget?: any }} event */
    askDelete(event) {
      const kind = KIND_OF_TAB[this.tab];
      const dialog = kind && this.item && this.catalog ? deleteImpact(this.catalog, kind, this.item) : null;
      if (!dialog) return;
      page = this;
      const name = dialog.name;
      saver?.dispose();
      saver = saveController({
        run: (options) => gateway[VERB_OF_KIND[kind]](name, options),
        onState: (state) => { page.delState = state; },
        timers,
      });
      saver.open(deleteBaseVersions(this.catalog, kind));
      this.delReturn = event?.currentTarget?.querySelector?.(":scope > button") ?? null;
      this.delTyped = "";
      this.del = dialog;
    },

    /** The dialog appeared: the keyboard starts in the name field. @param {HTMLElement} el */
    focusDelete(el) {
      el.querySelector("[data-delete-name]")?.focus();
    },

    /** Escape closes; Tab stays inside the dialog. @param {KeyboardEvent} event @param {{ activeElement: any }} [doc] */
    deleteKey(event, doc = document) {
      if (event.key === "Escape") {
        event.preventDefault();
        if (!this.delState.saving) this.cancelDelete();
        return;
      }
      if (event.key !== "Tab") return;
      const items = [...event.currentTarget.querySelectorAll(FOCUSABLE)];
      if (items.length === 0) return;
      const at = items.indexOf(doc.activeElement);
      if (event.shiftKey && at <= 0) {
        event.preventDefault();
        items[items.length - 1].focus();
      } else if (!event.shiftKey && (at === items.length - 1 || at < 0)) {
        event.preventDefault();
        items[0].focus();
      }
    },

    /** A press on the backdrop must not move the focus out of the dialog. @param {MouseEvent} event */
    deleteScrimDown(event) {
      if (event.target === event.currentTarget) event.preventDefault();
    },

    cancelDelete() {
      const back = this.delReturn;
      this.closeDelete();
      if (back?.isConnected) back.focus();
    },

    closeDelete() {
      saver?.dispose();
      this.del = null;
      this.delTyped = "";
      this.delReturn = null;
      this.delState = openSave({});
    },

    async confirmDelete() {
      if (!this.delReady || !saver) return;
      const { ok, result } = await saver.save();
      if (ok && result) await this.finishDelete(result);
    },

    /** The conflict banner's Overwrite: the same delete again, with force. */
    async delOverwrite() {
      if (!saver || this.isBroken) return;
      const { ok, result } = await saver.save({ force: true });
      if (ok && result) await this.finishDelete(result);
    },

    /** The conflict banner's Reload: read the catalog again, and keep the dialog with what the item reaches now. */
    async delReload() {
      if (!this.del) return;
      const { kind, name } = this.del;
      await this.load();
      const dialog = this.catalog ? deleteImpact(this.catalog, kind, name) : null;
      if (!this.del) return;
      if (!dialog) {
        this.closeDelete();
        this.notice = `That ${WORD_OF_KIND[kind]} no longer exists`;
        this.show(this.tab);
        return;
      }
      this.del = dialog;
      saver?.reload(deleteBaseVersions(this.catalog, kind));
    },

    /** Deleted: the toast with Undo, the library as it is now, and back to the item's tab. */
    async finishDelete(result) {
      const tab = this.tab;
      this.showToast(savedToast({ id: result.change.id, summary: result.change.summary }));
      this.closeDelete();
      await this.load();
      this.show(tab);
      this.$nextTick?.(() => document.getElementById(`agents-tab-${tab}`)?.focus());
    },
  };
}
