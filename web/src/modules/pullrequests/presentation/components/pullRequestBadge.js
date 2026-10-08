// One session's pull request, as a small badge that opens it on the host:
// <span x-data="pullRequestBadge(row.workspaceId)">. It reads the store the
// task page's pullRequestWatch fills; a session without a PR shows nothing.

import { toChip } from "../view.js";

/** @param {{ store: import("./pullRequestWatch.js").PullRequestStore }} deps */
export const pullRequestBadge = ({ store }) => (workspaceId = "") => ({
  get chip() {
    const pr = workspaceId ? store.byWorkspace[workspaceId] : null;
    return pr ? toChip(pr) : null;
  },
  get has() {
    return !!this.chip;
  },
  get href() {
    return this.chip?.href ?? "";
  },
  get label() {
    return this.chip?.short ?? "";
  },
  get state() {
    return this.chip?.state ?? "";
  },
  get tone() {
    return this.chip?.tone ?? null;
  },
  get describe() {
    return this.chip?.describe ?? "";
  },
});
