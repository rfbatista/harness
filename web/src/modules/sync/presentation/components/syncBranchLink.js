// A session row's "N branches" link on the task page, with a flag when one of
// the session's branches is unapproved. It reads the sync store's Branches
// lists (seeded by the page, kept live by the feed) and opens the Git popover
// at that session's Branches section.
//
//   <a x-data="syncBranchLink(row.id)" x-show="shown" href="#task-git" @click.prevent="open"> … </a>

import { branchesOf } from "../popoverView.js";

/** @param {{ store: import("./syncStore.js").SyncStore }} deps */
export const syncBranchLink = ({ store }) => (sessionId = "") => ({
  sessionId,

  get view() {
    return branchesOf(store.branches[this.sessionId]);
  },
  /** Hidden until the session's list is held: a session with no list has nothing to open. */
  get shown() {
    return this.view.shown;
  },
  get label() {
    return this.view.label;
  },
  get flagged() {
    return this.view.flagged;
  },
  get flagAttr() {
    return this.view.flagged ? "" : null;
  },
  get describe() {
    return this.view.flagged ? `${this.view.label}, ${this.view.flaggedLabel}` : this.view.label;
  },

  /** Asks the Git popover to open on this session's Branches section. */
  open() {
    this.$dispatch("branches-show", { sessionId: this.sessionId });
  },
});
