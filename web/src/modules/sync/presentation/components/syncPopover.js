// The task page's Git popover, in the title row beside Feature and Reviews:
//
//   <span style="display: contents" x-data="syncPopover">
//     <button id="task-git-button" x-show="shown" @click="openPopover">Git</button>
//     <div class="popup git-popover" id="task-git" popover="manual" x-data="popupDialog" …>
//       <template x-for="row in view.rows" :key="row.workspaceId"> …
//
// The worktrees come from the page the popover sits in (its sessions, agent and
// repository names, project and task: the sessions page's scope, read through
// $data) and their states from the sync store the page's syncWatch fills, so a
// session an agent starts while the page is open is listed once its state
// arrives. The open state is never stored: a page loads with it closed.

import { popoverOf } from "../popoverView.js";

const POPOVER_ID = "task-git";
const BUTTON_ID = "task-git-button";

/**
 * @param {{ store: import("./syncStore.js").SyncStore, actions: ReturnType<typeof import("./syncStore.js").syncActions>, now?: () => Date }} deps
 */
export const syncPopover = ({ store, actions, now = () => new Date() }) => () => ({
  /** The popover is open (browser-only). */
  popoverOpen: false,
  /** The workspace whose push question is asked in its row; one at a time. */
  confirming: "",

  /** @returns {ReturnType<typeof popoverOf>} */
  get view() {
    const page = this.$data;
    return popoverOf({
      sessions: page.sessions ?? [],
      states: store.byWorkspace,
      agentNames: page.agentNames ?? {},
      repositoryNames: page.repositoryNames ?? {},
      projectId: page.projectId ?? "",
      ticketId: page.ticketId ?? "",
      now: now(),
      fetching: store.fetching,
      pushing: store.pushing,
      confirming: this.confirming,
      pushFailure: store.pushFailure,
      fetchFailure: store.fetchFailure,
      branches: store.branches,
    });
  },
  get shown() {
    return this.view.shown;
  },
  get expandedAttr() {
    return this.popoverOpen ? "true" : "false";
  },
  get busyAttr() {
    return this.view.busy ? "true" : null;
  },
  get rows() {
    return this.view.rows;
  },
  get fetches() {
    return this.view.fetches;
  },
  get freshLine() {
    return this.view.fresh.line;
  },
  get freshOldAttr() {
    return this.view.fresh.old ? "" : null;
  },

  /** Opens the popover under the Git button; the dialog hands the focus back to it. */
  openPopover() {
    this.popoverOpen = true;
    this.$dispatch("dialog-open", { id: POPOVER_ID, anchor: document.getElementById(BUTTON_ID) });
  },
  /** A session row's "N branches" link: open at that session's Branches section. */
  showBranches(event) {
    const id = event?.detail?.sessionId;
    if (!this.popoverOpen) this.openPopover();
    this.$nextTick(() => {
      const section = [...document.querySelectorAll("#task-git [data-branches-for]")].find((el) => el.dataset.branchesFor === id);
      section?.scrollIntoView?.({ block: "nearest" });
      section?.focus?.();
    });
  },
  /** The dialog closed (Esc, Close or a press outside). */
  popoverClosed(event) {
    if (event?.detail?.id !== POPOVER_ID) return;
    this.popoverOpen = false;
    this.confirming = "";
  },

  askPush(workspaceId) {
    this.confirming = workspaceId;
  },
  cancelPush() {
    this.confirming = "";
  },
  async confirmPush(workspaceId) {
    if (this.confirming !== workspaceId) return;
    const ok = await actions.push(workspaceId);
    this.confirming = "";
    this.say(ok ? "Pushed." : this.view.rows.find((r) => r.workspaceId === workspaceId)?.refusal?.lead);
  },
  async fetch(repositoryId) {
    this.confirming = "";
    const ok = await actions.fetchRepository(repositoryId);
    this.say(ok ? "Fetched. Counts updated." : this.view.fetches.find((f) => f.repositoryId === repositoryId)?.refusal?.lead);
  },
  dismissPush(workspaceId) {
    const { [workspaceId]: _, ...rest } = store.pushFailure;
    store.pushFailure = rest;
  },
  dismissFetch(repositoryId) {
    const { [repositoryId]: _, ...rest } = store.fetchFailure;
    store.fetchFailure = rest;
  },

  /** Says it in the page's polite live region. */
  say(text) {
    if (text) this.$dispatch("announce", { text });
  },
});
