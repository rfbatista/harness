// A choice picker in the command palette's <dialog>: a filter, a listbox, and
// the keys a person expects (↑/↓ move, Enter picks, Esc closes). The design
// pages compose it to attach an asset to a task, or a task to an asset:
//
//   compose(artifactBrowsing(clock), picker(), { …, openAttachPicker() { this.openPicker({…}) } })
//
//   <dialog class="[ palette ]" x-ref="picker" x-bind:aria-label="pickerLabel" x-on:close="pickerClosed" …>
//     <input x-ref="pickerInput" role="combobox" x-model="pickerQuery" …>
//     <div class="[ results ]" role="listbox"><template x-for="choice in pickerResults">…</template></div>
//   </dialog>
//
// The dialog is modal (showModal), so focus stays in it; on close focus goes
// back to the control that opened it.

import { filterChoices } from "../artifactView.js";

/** @typedef {{ id: string, label: string, detail?: string }} Choice */

export function picker() {
  /** @type {(id: string) => void} */
  let onPick = () => {};
  /** @type {HTMLElement | null} */
  let opener = null;

  return {
    pickerOpen: false,
    pickerLabel: "",
    pickerEmptyText: "",
    pickerQuery: "",
    /** @type {Choice[]} */
    pickerChoices: [],
    pickerActiveId: "",

    get pickerResults() {
      return filterChoices(this.pickerChoices, this.pickerQuery).map((c) => ({
        ...c,
        domId: pickerDomId(c.id),
        active: c.id === this.pickerActiveId,
      }));
    },
    get pickerIsEmpty() {
      return this.pickerResults.length === 0;
    },
    /** The input's aria-activedescendant: the option the arrows are on. */
    get pickerActiveDomId() {
      return this.pickerResults.some((c) => c.active) ? pickerDomId(this.pickerActiveId) : "";
    },

    /**
     * @param {{ label: string, emptyText: string, choices: Choice[], onPick: (id: string) => void }} options
     */
    openPicker({ label, emptyText, choices, onPick: pick }) {
      opener = /** @type {HTMLElement | null} */ (this.$el.ownerDocument?.activeElement ?? null);
      onPick = pick;
      this.pickerLabel = label;
      this.pickerEmptyText = emptyText;
      this.pickerChoices = choices;
      this.pickerQuery = "";
      this.pickerActiveId = choices[0]?.id ?? "";
      this.pickerOpen = true;
      this.$nextTick(() => {
        const dialog = this.$refs.picker;
        if (dialog && !dialog.open) dialog.showModal?.();
        this.$refs.pickerInput?.focus?.();
      });
    },

    /** The filter changed: the arrows start again from the first match. */
    pickerFiltered() {
      this.pickerActiveId = this.pickerResults[0]?.id ?? "";
    },
    pickerNext() {
      this.pickerStep(1);
    },
    pickerPrevious() {
      this.pickerStep(-1);
    },
    pickerStep(delta) {
      const results = this.pickerResults;
      if (results.length === 0) return;
      const at = results.findIndex((c) => c.active);
      const to = at === -1 ? 0 : Math.max(0, Math.min(results.length - 1, at + delta));
      this.pickerActiveId = results[to].id;
    },
    pickerChoose() {
      if (this.pickerResults.some((c) => c.active)) this.pick(this.pickerActiveId);
    },
    /** @param {string} id */
    pick(id) {
      const chosen = onPick;
      this.closePicker();
      chosen(id);
    },
    closePicker() {
      const dialog = this.$refs.picker;
      if (dialog?.open) dialog.close();
      else this.pickerClosed();
    },
    /** The dialog closed (Esc, a pick, or closePicker): forget it and give focus back. */
    pickerClosed() {
      if (!this.pickerOpen) return;
      this.pickerOpen = false;
      this.pickerChoices = [];
      onPick = () => {};
      opener?.focus?.();
      opener = null;
    },
  };
}

const pickerDomId = (id) => `picker-option-${id}`;
