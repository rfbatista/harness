// A repository's or task's own orçamento de memória, in GB. An empty field
// inherits. The server paints the owner and the values; a change saves at
// once and the summary follows the answer.
//
//   <div x-data="memoryBudgetOverride" data-level="repository" data-owner-id="r1"
//        data-bytes="0" data-effective="4294967296">
//     <input type="number" x-on:change="choose"> …

import { Codes, StructuredError } from "../../../../shared/domain/errors.js";
import { describeError } from "../../../../shared/presentation/errors.js";
import { toBytes, toGb } from "../../domain/memory-budget.js";

/** @param {{ gateway: import("../../domain/ports.js").SettingsGateway }} deps */
export const memoryBudgetOverride = ({ gateway }) => () => ({
  level: "",
  ownerId: "",
  /** The owner's own orçamento in bytes; 0 while it inherits. */
  bytes: 0,
  /** What a session there gets, in bytes. */
  effective: 0,
  saving: false,
  error: null,

  init() {
    const d = this.$el.dataset;
    this.level = d.level ?? "";
    this.ownerId = d.ownerId ?? "";
    this.bytes = Number(d.bytes) || 0;
    this.effective = Number(d.effective) || 0;
  },

  get summary() {
    return this.bytes ? `Uses ${toGb(this.bytes)} GB` : `Inherits ${toGb(this.effective)} GB`;
  },

  /** @param {Event} event the field's change */
  async choose(event) {
    const input = /** @type {HTMLInputElement} */ (event.target);
    const typed = input.value.trim();
    const bytes = typed === "" ? 0 : toBytes(typed);
    this.error = null;
    if (bytes === null) {
      this.error = describeError(new StructuredError(Codes.INVALID_INPUT, "Enter a number of GB above zero, or leave it empty to inherit."));
      input.value = this.bytes ? toGb(this.bytes) : "";
      return;
    }
    if (this.saving || bytes === this.bytes) return;
    this.saving = true;
    try {
      const saved = await gateway.setMemoryBudgetOverride(this.level, this.ownerId, bytes);
      this.bytes = saved.bytes;
      this.effective = saved.effectiveBytes;
    } catch (err) {
      this.error = describeError(err);
      input.value = this.bytes ? toGb(this.bytes) : "";
    } finally {
      this.saving = false;
    }
  },

  dismissError() {
    this.error = null;
  },
});
