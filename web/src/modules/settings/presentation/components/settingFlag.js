// One boolean global setting as a checkbox on the Server page: the server
// paints it at the stored value, and a change saves at once; a refused one
// puts the checkbox back.
//
//   <div x-data="settingFlag" data-setting="documents.agents_move_to_project">
//     <input type="checkbox" x-on:change="choose"> …

import { describeError } from "../../../../shared/presentation/errors.js";

/** @param {{ gateway: import("../../domain/ports.js").SettingsGateway }} deps */
export const settingFlag = ({ gateway }) => () => ({
  /** The settings key this flag is stored under. */
  key: "",
  /** The value the server holds. */
  value: false,
  saving: false,
  error: null,
  announcement: "",

  init() {
    this.key = this.$el.dataset.setting ?? "";
    this.value = this.$el.querySelector("input")?.checked ?? false;
  },

  /** @param {Event} event the checkbox's change */
  async choose(event) {
    const box = /** @type {HTMLInputElement} */ (event.target);
    if (this.saving || box.checked === this.value) return;
    this.saving = true;
    this.error = null;
    try {
      this.value = await gateway.setFlag(this.key, box.checked);
      this.announcement = this.value ? "Setting saved: on." : "Setting saved: off.";
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
