// The "Uses the design system" switch and path of the project's repositories,
// one row each, on the design system page's Repositories tab (ticket 15 redoes
// it by design system). A repository's own settings use repositoryDesignSystem.
//
//   <div x-data="designSystemOptIn" data-project-id="p1">
//
// Switching saves at once, through set_repository_design_system; the path is
// saved with its own button. Switching on a repository that uses another
// design system asks to move it first. A refusal stays under its row.

import { describeError } from "../../../../shared/presentation/errors.js";
import { EFFECT_NOTE, exportLabel } from "../../domain/repositories.js";

/** @param {{ gateway: ReturnType<typeof import("../../infrastructure/designsystem-gateway.js").designSystemGateway> }} deps */
export const designSystemOptIn = ({ gateway }) => () => ({
  projectId: "",
  /** The design system the page names (BRIDGE, ticket 15: "" on a page that names none: the project's first). */
  designSystemId: "",
  /** @type {ReturnType<typeof import("../../domain/repositories.js").optInRow>[]} */
  rows: [],
  loaded: false,
  error: null,
  effectNote: EFFECT_NOTE,

  get isEmpty() {
    return this.loaded && this.rows.length === 0;
  },

  async init() {
    this.projectId = this.$el.dataset.projectId ?? "";
    this.designSystemId = this.$el.dataset.designSystemId ?? "";
    await this.load();
  },

  async load() {
    this.error = null;
    try {
      this.rows = await gateway.repositories(this.projectId, this.designSystemId);
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
  /** Which other design system the repository uses, for the note beside an unset row. */
  elsewhere(row) {
    return !row.uses && row.appliedName !== "" ? "Uses " + row.appliedName + " now." : "";
  },
  cannotSavePath(row) {
    return row.saving || !row.uses || row.draft === row.path;
  },

  /** Flips the switch. Switching on carries the path as drafted; switching off keeps the saved one, so a path half typed never blocks it. */
  async toggle(row) {
    if (!row.uses && row.appliedId !== "") {
      row.uses = true;
      row.confirming = true;
      return;
    }
    await this.save(row, !row.uses);
  },

  /** Moves the repository from the design system it uses to this one. */
  async confirmMove(row) {
    row.confirming = false;
    row.uses = false;
    await this.save(row, true, true);
  },

  keepCurrent(row) {
    row.confirming = false;
    row.uses = false;
  },

  async savePath(row) {
    if (this.cannotSavePath(row)) return;
    await this.save(row, true);
  },

  async save(row, uses, confirmMove = false) {
    if (row.saving) return;
    const before = row.uses;
    row.saving = true;
    row.error = null;
    row.uses = uses; // the switch already moved under the person's hand
    try {
      const stored = await gateway.setRepository(row.id, uses, uses ? row.draft : row.path, this.projectId, confirmMove, this.designSystemId);
      row.uses = stored.uses;
      row.path = stored.path || row.path;
      row.draft = row.path;
      row.appliedId = stored.designSystemId;
      if (row.appliedId === "") row.appliedName = "";
    } catch (err) {
      row.uses = before;
      row.error = describeError(err);
    } finally {
      row.saving = false;
    }
  },
});
