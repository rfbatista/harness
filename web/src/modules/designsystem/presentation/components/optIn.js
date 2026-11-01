// The "Uses the design system" switch and path of repositories, one row each:
// on the design system page's Repositories tab for the whole project, and on a
// repository's settings for that repository alone.
//
//   <div x-data="designSystemOptIn" data-project-id="p1" [data-repository-id="r1"]>
//
// Switching saves at once, through the same endpoint from both places; the
// path is saved with its own button. A refusal stays under its row.

import { describeError } from "../../../../shared/presentation/errors.js";
import { EFFECT_NOTE, exportLabel } from "../../domain/repositories.js";

/** @param {{ gateway: ReturnType<typeof import("../../infrastructure/designsystem-gateway.js").designSystemGateway> }} deps */
export const designSystemOptIn = ({ gateway }) => () => ({
  projectId: "",
  /** The one repository this shows (empty: all of the project's). */
  repositoryId: "",
  /** @type {ReturnType<typeof import("../../domain/repositories.js").optInRow>[]} */
  rows: [],
  loaded: false,
  error: null,
  effectNote: EFFECT_NOTE,

  get isEmpty() {
    return this.loaded && this.rows.length === 0;
  },
  get single() {
    return this.repositoryId !== "";
  },

  async init() {
    this.projectId = this.$el.dataset.projectId ?? "";
    this.repositoryId = this.$el.dataset.repositoryId ?? "";
    await this.load();
  },

  async load() {
    this.error = null;
    try {
      const all = await gateway.repositories(this.projectId);
      this.rows = this.single ? all.filter((r) => r.id === this.repositoryId) : all;
      this.loaded = true;
    } catch (err) {
      this.error = describeError(err);
    }
  },

  phase(row) {
    return row.saving ? "saving" : "idle";
  },
  stateOf(row) {
    return exportLabel(row);
  },
  cannotSavePath(row) {
    return row.saving || !row.uses || row.draft === row.path;
  },

  /** Flips the switch. Switching on carries the path as drafted; switching off keeps the saved one, so a path half typed never blocks it. */
  async toggle(row) {
    await this.save(row, !row.uses);
  },

  async savePath(row) {
    if (this.cannotSavePath(row)) return;
    await this.save(row, true);
  },

  async save(row, uses) {
    if (row.saving) return;
    const before = row.uses;
    row.saving = true;
    row.error = null;
    row.uses = uses; // the switch already moved under the person's hand
    try {
      const stored = await gateway.setRepository(row.id, uses, uses ? row.draft : row.path);
      row.uses = stored.uses;
      row.path = stored.path || row.path;
      row.draft = row.path;
    } catch (err) {
      row.uses = before;
      row.error = describeError(err);
    } finally {
      row.saving = false;
    }
  },
});
