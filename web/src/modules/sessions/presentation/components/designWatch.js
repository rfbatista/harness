// The task toolbar's “Design assets (n)” link: n is the task's design assets
// (what it made and what is attached to it), counted from the page's seed and
// kept current over the project feed as assets are attached, detached, moved
// back or deleted. A new task-scope publish is not on that feed; it counts
// from the next load (the Design tab shows it live).
//
//   <a x-data="sessionsDesignWatch" data-seed="sessions-seed" href="…/design">
//     Design assets <span class="[ badge ]" x-text="count">2</span></a>

import { readSeed } from "../../../../shared/presentation/seed.js";
import { belongsTo } from "../../domain/artifact.js";

/** @param {{ artifacts: import("../../domain/ports.js").ArtifactGateway }} deps */
export const designWatch = ({ artifacts }) => () => {
  let unfollow = () => {};

  return {
    ticketId: "",
    /** The task's assets by id. */
    ids: [],

    get count() {
      return String(this.ids.length);
    },

    init() {
      const seed = readSeed(this.$el);
      this.ticketId = seed?.ticket_id ?? "";
      try {
        this.ids = artifacts.decodeArtifacts(seed?.artifacts ?? []).filter((a) => belongsTo(a, this.ticketId)).map((a) => a.id);
      } catch {
        this.ids = [];
      }
      if (!seed?.project_id || !this.ticketId) return;
      unfollow = artifacts.followProject(seed.project_id, (change) => this.onProjectChange(change), () => {});
    },

    destroy() {
      unfollow();
    },

    /** @param {import("../../domain/ports.js").ProjectArtifactChange} change */
    onProjectChange(change) {
      const id = change.kind === "deleted" ? change.id : change.artifact.id;
      const others = this.ids.filter((x) => x !== id);
      this.ids = change.kind === "changed" && belongsTo(change.artifact, this.ticketId) ? [...others, id] : others;
    },
  };
};
