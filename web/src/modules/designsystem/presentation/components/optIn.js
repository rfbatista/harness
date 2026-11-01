// The Repositories tab of a design system: every repository of the project,
// one row each, with the design system it uses (a chip), its path, and Apply
// here or Remove. A repository's own settings use repositoryDesignSystem.
//
//   <div x-data="designSystemOptIn" data-project-id="p1" data-design-system-id="ds1">
//
// Both act at once, through set_repository_design_system. Applying a
// repository that uses another design system is refused by the server
// (MOVE_NOT_CONFIRMED); the row then asks to move it, naming the current one,
// and retries confirmed. Any other refusal stays under its row.

import { describeError } from "../../../../shared/presentation/errors.js";
import { chip, exportLabel } from "../../domain/repositories.js";

/** @param {{ gateway: ReturnType<typeof import("../../infrastructure/designsystem-gateway.js").designSystemGateway> }} deps */
export const designSystemOptIn = ({ gateway }) => () => ({
  projectId: "",
  /** The design system the page is about. */
  designSystemId: "",
  /** @type {ReturnType<typeof import("../../domain/repositories.js").optInRow>[]} */
  rows: [],
  loaded: false,
  error: null,

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
      this.rows = await gateway.repositories(this.designSystemId);
      this.loaded = true;
    } catch (err) {
      this.error = describeError(err);
    }
  },

  phase(row) {
    return row.saving ? "saving" : "idle";
  },
  chipOf(row) {
    return chip(row);
  },
  stateOf(row) {
    return exportLabel(row);
  },
  cannotSavePath(row) {
    return row.saving || !row.uses || row.draft === row.path;
  },

  /** Applies this design system with the path as drafted; one that uses another asks to move first. */
  async apply(row) {
    await this.save(row, this.designSystemId, row.draft, false);
  },

  /** Moves the repository from the design system it uses to this one. */
  async confirmMove(row) {
    row.confirming = false;
    await this.save(row, this.designSystemId, row.draft, true);
  },

  keepCurrent(row) {
    row.confirming = false;
  },

  /** Removes the design system in use; the saved path is sent so a half typed one is never stored. */
  async remove(row) {
    await this.save(row, null, row.path, false);
  },

  async savePath(row) {
    if (this.cannotSavePath(row)) return;
    await this.apply(row);
  },

  async save(row, designSystemId, path, confirmMove) {
    if (row.saving) return;
    row.saving = true;
    row.error = null;
    try {
      const stored = await gateway.apply(row.id, designSystemId, path, confirmMove);
      row.uses = stored.designSystemId === this.designSystemId;
      row.appliedId = stored.designSystemId;
      if (stored.designSystemId === "") row.appliedName = "";
      row.path = stored.path || row.path;
      row.draft = row.path;
      row.confirming = false;
    } catch (err) {
      if (err?.code === "MOVE_NOT_CONFIRMED") {
        row.movingFrom = err.details?.current?.name || row.appliedName;
        row.confirming = true;
      } else {
        row.error = describeError(err);
      }
    } finally {
      row.saving = false;
    }
  },
});
