// One tier's model on the Models page: a select the server paints with the
// stored model, and the settings key it saves to. A change saves at once; a
// refused one puts the select back.
//
//   <div x-data="modelTier" data-setting="models.claude.architect"> … <select x-on:change="choose"> …

import { describeError } from "../../../../shared/presentation/errors.js";

/** @param {{ gateway: import("../../domain/ports.js").ModelGateway }} deps */
export const modelTier = ({ gateway }) => () => ({
  /** The settings key this tier is stored under. */
  key: "",
  /** The model the server holds. */
  value: "",
  saving: false,
  error: null,
  announcement: "",

  init() {
    this.key = this.$el.dataset.setting ?? "";
    this.value = this.$el.querySelector("select")?.value ?? "";
  },

  /** @param {Event} event the select's change */
  async choose(event) {
    const select = /** @type {HTMLSelectElement} */ (event.target);
    if (this.saving || select.value === this.value) return;
    this.saving = true;
    this.error = null;
    try {
      this.value = await gateway.setModel(this.key, /** @type {any} */ (select.value));
      this.announcement = `Model set to ${this.value}.`;
    } catch (err) {
      this.error = describeError(err);
      select.value = this.value;
    } finally {
      this.saving = false;
    }
  },

  dismissError() {
    this.error = null;
  },
});
