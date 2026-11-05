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
import { followSkeleton, skeletonFields } from "../../../../shared/presentation/skeleton.js";
import { chip, exportBadge, exportLabel } from "../../domain/repositories.js";

/** @param {{ gateway: ReturnType<typeof import("../../infrastructure/designsystem-gateway.js").designSystemGateway> }} deps */
export const designSystemOptIn = ({ gateway }) => () => {
  /** When the first read shows a skeleton (shared rule). */
  let skeleton = null;
  return {
  ...skeletonFields(),
  projectId: "",
  /** The design system the page is about. */
  designSystemId: "",
  /** @type {ReturnType<typeof import("../../domain/repositories.js").optInRow>[]} */
  rows: [],
  loaded: false,
  error: null,
  unfollow: null,
  /** The repository the header's Export… chose. */
  chosen: "",

  get isEmpty() {
    return this.loaded && this.rows.length === 0;
  },

  async init() {
    skeleton = followSkeleton(this);
    this.projectId = this.$el.dataset.projectId ?? "";
    this.designSystemId = this.$el.dataset.designSystemId ?? "";
    await this.load();
    this.unfollow = gateway.follow?.(this.projectId, this.designSystemId, {
      onSaved: () => this.refreshExports(),
      onExport: () => this.refreshExports(),
    }) ?? null;
  },

  destroy() {
    this.unfollow?.();
    skeleton?.stop();
  },

  /** Reads the export states again, leaving what a row is doing (a draft path, a question, an export) alone. */
  async refreshExports(force = false) {
    try {
      const fresh = await gateway.repositories(this.designSystemId);
      for (const row of this.rows) {
        const now = fresh.find((f) => f.id === row.id);
        if (now && !row.saving && (force || !row.exporting)) row.export = now.export;
      }
    } catch {
      // The next event, or Try again, reads them.
    }
  },

  async load() {
    this.error = null;
    if (!this.loaded) skeleton?.begin();
    try {
      const rows = await gateway.repositories(this.designSystemId);
      // The rows come in once the skeleton is gone, so they never stack under it.
      await skeleton?.end();
      this.rows = rows;
      this.loaded = true;
    } catch (err) {
      skeleton?.fail();
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
  badgeOf(row) {
    return exportBadge(row) ?? { label: "", tone: "" };
  },
  hasBadge(row) {
    return exportBadge(row) !== null;
  },
  exportDisabled(row) {
    return row.exporting || row.saving || row.review !== null;
  },
  isReviewing(row) {
    return row.review !== null;
  },
  reviewFiles(row) {
    return row.review === null ? [] : row.review;
  },
  currentVersion(row) {
    return row.export === null ? "" : row.export.current_version;
  },
  shortCommit(row) {
    return row.exported === null ? "" : row.exported.commit.slice(0, 7);
  },
  isChosen(row) {
    return row.id === this.chosen;
  },
  /** The header's Export…: chooses the first repository that needs an export, else the first that uses the design system. */
  choose() {
    const using = this.rows.filter((r) => r.uses);
    const pick = using.find((r) => r.export?.state !== "in_sync") ?? using[0];
    this.chosen = pick?.id ?? "";
    if (pick) {
      this.$nextTick(() => this.$el.querySelector(`[data-optin-row][data-repository-id="${pick.id}"] [data-action=export]`)?.focus());
    }
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

  /** Exports one repository. Files edited outside the harness are listed first, with their differing lines, and replaced only once confirmed. */
  async exportRow(row) {
    if (row.exporting) return;
    row.exporting = true;
    row.exportError = null;
    row.exported = null;
    try {
      if (row.export?.state === "edited_outside" && row.review === null) {
        await this.openReview(row);
        return;
      }
      await this.runExport(row, row.review !== null);
    } catch (err) {
      row.exportError = describeError(err);
    } finally {
      row.exporting = false;
    }
  },

  async runExport(row, confirmed) {
    try {
      row.exported = await gateway.exportDesignSystem(row.id, confirmed);
      row.review = null;
      await this.refreshExports(true);
    } catch (err) {
      if (err?.code === "EDITED_OUTSIDE" && !confirmed) {
        await this.openReview(row);
        return;
      }
      throw err;
    }
  },

  /** Lists the files that differ, with their lines, under the row. */
  async openReview(row) {
    const files = await gateway.previewExport(row.id);
    row.review = files.filter((f) => f.status !== "unchanged");
  },

  /** Withdraws the edited outside question; nothing was written. */
  cancelReview(row) {
    row.review = null;
  },

};
};
