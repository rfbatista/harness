// The global orçamento de memória da sessão as a field on the Server page,
// in GB: the server paints it at the stored value, and a change saves at
// once; a bad or refused value puts the field back.
//
//   <div x-data="memoryBudget" data-bytes="4294967296">
//     <input type="number" x-on:change="choose"> …

import { Codes, StructuredError } from "../../../../shared/domain/errors.js";
import { describeError } from "../../../../shared/presentation/errors.js";
import { toBytes, toGb } from "../../domain/memory-budget.js";

/** @param {{ gateway: import("../../domain/ports.js").SettingsGateway }} deps */
export const memoryBudget = ({ gateway }) => () => ({
  /** The orçamento the server holds, in bytes. */
  value: 0,
  saving: false,
  error: null,
  announcement: "",

  init() {
    this.value = Number(this.$el.dataset.bytes) || 0;
  },

  /** @param {Event} event the field's change */
  async choose(event) {
    const input = /** @type {HTMLInputElement} */ (event.target);
    const bytes = toBytes(input.value);
    this.error = null;
    if (bytes === null) {
      this.error = describeError(new StructuredError(Codes.INVALID_INPUT, "Enter a number of GB above zero."));
      input.value = toGb(this.value);
      return;
    }
    if (this.saving || bytes === this.value) return;
    this.saving = true;
    try {
      this.value = await gateway.setMemoryBudget(bytes);
      input.value = toGb(this.value);
      this.announcement = `Memory budget saved: ${toGb(this.value)} GB.`;
    } catch (err) {
      this.error = describeError(err);
      input.value = toGb(this.value);
    } finally {
      this.saving = false;
    }
  },

  dismissError() {
    this.error = null;
  },
});
