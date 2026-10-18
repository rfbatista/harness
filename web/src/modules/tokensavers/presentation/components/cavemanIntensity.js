// The Token savers page's Caveman intensity section: one select, painted by
// the server with the current intensity. A change saves at once; a refused
// one puts the select back.
//
//   <section x-data="cavemanIntensity"> … <select x-on:change="choose"> …

import { describeError } from "../../../../shared/presentation/errors.js";

/** @param {{ gateway: import("../../domain/caveman-intensity-ports.js").CavemanIntensityGateway }} deps */
export const cavemanIntensity = ({ gateway }) => () => ({
  /** The intensity the server holds. */
  value: "",
  saving: false,
  error: null,
  announcement: "",

  init() {
    this.value = this.$el.querySelector("select")?.value ?? "";
  },

  /** @param {Event} event the select's change */
  async choose(event) {
    const select = /** @type {HTMLSelectElement} */ (event.target);
    if (this.saving || select.value === this.value) return;
    this.saving = true;
    this.error = null;
    try {
      this.value = await gateway.set(/** @type {any} */ (select.value));
      this.announcement = `Caveman intensity set to ${this.value}.`;
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
