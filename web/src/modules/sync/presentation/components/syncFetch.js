// One Fetch per repository, for the task page's Git popover foot:
//
//   <button x-data="syncFetch('<repository id>')" data-fetch @click="fetch" :aria-disabled="disabledAttr" x-text="label">
//
// The age line beside it is the oldest fetch among the repository's worktrees
// (a fetch from any session moves it for all).

import { fetchAge } from "../view.js";

/** @param {{ store: import("./syncStore.js").SyncStore, actions: ReturnType<typeof import("./syncStore.js").syncActions>, now?: () => Date }} deps */
export const syncFetch = ({ store, actions, now = () => new Date() }) => (repositoryId = "") => ({
  repositoryId,

  get busy() {
    return !!store.fetching[this.repositoryId];
  },
  get label() {
    return this.busy ? "Fetching…" : "↻ Fetch";
  },
  get disabledAttr() {
    return this.busy ? "true" : null;
  },
  get workspaces() {
    return Object.values(store.byWorkspace).filter((s) => s.repositoryId === this.repositoryId);
  },
  /** The oldest fetch among the repository's worktrees; null when any was never fetched. */
  get fetchedAt() {
    const times = this.workspaces.map((s) => s.fetchedAt);
    if (times.length === 0 || times.some((t) => !t)) return null;
    return new Date(Math.min(...times.map((t) => t.getTime())));
  },
  get freshLine() {
    const age = fetchAge(this.fetchedAt, now());
    return age.never ? "Not fetched yet" : `Fetched ${age.ago}`;
  },
  get freshOldAttr() {
    return fetchAge(this.fetchedAt, now()).old ? "" : null;
  },
  get refusal() {
    return store.fetchFailure[this.repositoryId] ?? null;
  },
  get hasRefusal() {
    return !!this.refusal;
  },
  get refusalLead() {
    return this.refusal?.lead ?? "";
  },
  get refusalHref() {
    return this.refusal?.href ?? "";
  },
  get refusalLinkText() {
    return this.refusal?.linkText ?? "";
  },

  async fetch() {
    await actions.fetchRepository(this.repositoryId);
  },
});
