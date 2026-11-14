// One session mode's branch block on the Server page: a checkbox the server
// paints at the stored value, and the settings key it saves to. A change saves
// at once as "on" or "off"; a refused one puts the checkbox back.
//
//   <li x-data="branchBlock" data-setting="branches.block.mode.implement">
//     <input type="checkbox" x-on:change="choose"> …

import { describeError } from "../../../../shared/presentation/errors.js";

/** @param {{ gateway: import("../../domain/ports.js").SettingsGateway }} deps */
export const branchBlock = ({ gateway }) => () => ({
  /** The settings key this mode's block is stored under. */
  key: "",
  /** Whether the server holds the block on. */
  value: true,
  saving: false,
  error: null,
  announcement: "",

  init() {
    this.key = this.$el.dataset.setting ?? "";
    this.value = this.$el.querySelector("input")?.checked ?? true;
  },

  /** @param {Event} event the checkbox's change */
  async choose(event) {
    const box = /** @type {HTMLInputElement} */ (event.target);
    if (this.saving || box.checked === this.value) return;
    this.saving = true;
    this.error = null;
    try {
      this.value = await gateway.setBranchBlock(this.key, box.checked);
      this.announcement = this.value ? "Branch block saved: on." : "Branch block saved: off.";
    } catch (err) {
      this.error = describeError(err);
      box.checked = this.value;
    } finally {
      this.saving = false;
    }
  },

  dismissError() {
    this.error = null;
  },
});
