// A worktree's sync-state chip, bound to the shared store the page's
// syncWatch fills:
//
//   <span class="sync-state" x-data="syncChip('<workspace id>')" x-show="has"
//         :data-state="state" :data-loading="loadingAttr" :data-old="oldAttr" :title="title">
//     <span class="upstream" x-show="hasUpstream" x-text="upstream"></span>
//     <span class="counts" x-show="hasCounts">
//       <span class="ahead"><span aria-hidden="true">↑</span><span x-text="ahead"></span><span class="visually-hidden"> ahead</span></span> …
//     <span class="word" x-show="hasWord" x-text="word"></span>
//
// It renders the same words the server paints (the templ chip): read-only, so
// it serves the session rows, the History line and the popover's rows.

import { canPush, isOld } from "../../domain/gitState.js";
import { chipOf, panelOf, uncommittedLine } from "../view.js";

/**
 * The skeleton chip a session row draws, in the chip's place, while the git
 * section loads and the row's worktree has no state yet.
 *
 *   <span class="skeleton" data-variant="chip" x-data="syncChipSkeleton(row.workspaceId)" x-show="shown" …>
 *
 * @param {{ store: import("./syncStore.js").SyncStore }} deps
 */
export const syncChipSkeleton = ({ store }) => (workspaceId = "") => ({
  get shown() {
    return store.statesLoading && workspaceId !== "" && !store.byWorkspace[workspaceId];
  },
});

/**
 * @param {{ store: import("./syncStore.js").SyncStore, actions: ReturnType<typeof import("./syncStore.js").syncActions>, now?: () => Date }} deps
 */
export const syncChip = ({ store, actions, now = () => new Date() }) => (workspaceId = "") => ({
  workspaceId,

  /** The chip fades in where its skeleton chip was. */
  get fadeClass() {
    return store.statesIn;
  },

  /** @returns {import("../../domain/gitState.js").GitState | null} */
  get gitState() {
    return store.byWorkspace[this.workspaceId] ?? null;
  },
  get chip() {
    return this.gitState ? chipOf(this.gitState, { now: now(), busy: actions.busy(this.gitState) }) : null;
  },
  get has() {
    return !!this.gitState;
  },
  /** The session row shows the chip: a state is held and its worktree is on disk. */
  get shown() {
    return !!this.gitState && !this.gitState.gone;
  },
  get state() {
    return this.chip?.state ?? "";
  },
  get upstream() {
    return this.chip?.upstream ?? "";
  },
  get hasUpstream() {
    return this.chip?.hasUpstream ?? false;
  },
  get title() {
    return this.chip?.title ?? "";
  },
  get hasCounts() {
    return this.chip?.hasCounts ?? false;
  },
  get ahead() {
    return this.chip?.ahead ?? 0;
  },
  get behind() {
    return this.chip?.behind ?? 0;
  },
  get word() {
    return this.chip?.word ?? "";
  },
  get hasWord() {
    return this.chip?.hasWord ?? false;
  },
  /** data-loading while a fetch or a push covers the workspace ("" is no attribute: null). */
  get loadingAttr() {
    return this.chip?.loading ? "" : null;
  },
  /** data-old when the counts are over an hour behind. */
  get oldAttr() {
    return this.chip?.old ? "" : null;
  },
  get describe() {
    return this.chip?.describe ?? "";
  },

  // The rest of a popover row: the base comparison, the uncommitted words, Code link, Push.
  get base() {
    return this.gitState ? panelOf(this.gitState, { now: now() }).base : null;
  },
  get baseState() {
    return this.base?.chip.state ?? "";
  },
  get baseAhead() {
    return this.base?.chip.ahead ?? 0;
  },
  get baseBehind() {
    return this.base?.chip.behind ?? 0;
  },
  get baseHasCounts() {
    return this.base?.chip.hasCounts ?? false;
  },
  get baseWord() {
    return this.base?.chip.word ?? "";
  },
  get baseHasWord() {
    return this.base?.chip.hasWord ?? false;
  },
  get branch() {
    return this.gitState?.branch ?? "";
  },
  get uncommitted() {
    return this.gitState ? uncommittedLine(this.gitState.uncommitted) : "";
  },
  get gone() {
    return this.gitState?.gone ?? false;
  },
  get offersPush() {
    return !!this.gitState && canPush(this.gitState);
  },
  /** The fetch age is old (amber). */
  get old() {
    return !!this.gitState && isOld(this.gitState.fetchedAt ?? this.gitState.pushedAt, now());
  },
});
