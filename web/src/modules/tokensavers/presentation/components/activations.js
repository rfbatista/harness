// The Token savers control of a project's Settings page, a repository's
// settings page and a task's detail: per saver, its value at this level
// (a select) and the server's line on what it resolves to. A change saves
// at once and the line follows the answer.
//
//   <section x-data="tokenSaversActivations" data-seed="token-savers-seed"> …

import { describeError } from "../../../../shared/presentation/errors.js";
import { readSeed } from "../../../../shared/presentation/seed.js";

/** @param {{ gateway: import("../../domain/ports.js").TokenSaverGateway }} deps */
export const activations = ({ gateway }) => () => ({
  level: "",
  ownerId: "",
  /** @type {{ saver: string, value: string, summary: string, inputId: string, saving: boolean }[]} */
  savers: [],
  ready: false,
  error: null,

  init() {
    try {
      this.apply(gateway.decodeSeed(readSeed(this.$el)));
    } catch (err) {
      this.error = describeError(err);
      return;
    }
    this.$nextTick(() => {
      for (const node of this.$el.querySelectorAll("[data-ssr]")) node.remove();
      this.ready = true;
    });
  },

  /** @param {Event} event the change of one saver's select */
  async choose(event) {
    const select = /** @type {HTMLSelectElement} */ (event.target);
    const row = this.savers.find((s) => s.saver === select.closest("[data-saver]")?.dataset.saver);
    if (!row || row.saving || select.value === row.value) return;
    row.saving = true;
    this.error = null;
    try {
      this.apply(await gateway.set(this.level, this.ownerId, row.saver, select.value));
    } catch (err) {
      this.error = describeError(err);
      select.value = row.value;
    } finally {
      row.saving = false;
    }
  },

  /** @param {import("../../domain/ports.js").Activations} a */
  apply(a) {
    this.level = a.level;
    this.ownerId = a.ownerId;
    const saving = new Map(this.savers.map((s) => [s.saver, s.saving]));
    this.savers = a.activations.map((x) => ({
      saver: x.saver,
      value: x.value,
      summary: x.summary,
      inputId: `token-saver-${a.level}-${x.saver}`,
      saving: saving.get(x.saver) ?? false,
    }));
  },

  dismiss() {
    this.error = null;
  },
});
