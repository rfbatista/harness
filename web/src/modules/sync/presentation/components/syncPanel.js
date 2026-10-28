// The sync panel a chip opens: both comparisons, the fetch age, Fetch and
// Push behind a confirmation. It is the chip's own bindings plus the panel's,
// so one x-data covers the chip button and the popup beside it:
//
//   <span class="sync-slot" x-data="syncPanel('<workspace id>')" @keydown.escape="close">
//     <button class="sync-state" :data-state="state" aria-haspopup="dialog" :aria-expanded="expandedAttr" @click="toggle"> … </button>
//     <div class="popup sync-panel" role="dialog" aria-label="Git state" :aria-busy="busyAttr" x-show="open"> …
//
// Placement of the popup, its focus trap and Escape return are the page
// wiring's (they need a browser); the open state is never stored.

import { syncChip } from "./syncChip.js";
import { fetchedPhrase, panelOf } from "../view.js";

/**
 * @param {{ store: import("./syncStore.js").SyncStore, actions: ReturnType<typeof import("./syncStore.js").syncActions>, now?: () => Date }} deps
 */
export const syncPanel = (deps) => (workspaceId = "") => {
  const { store, actions, now = () => new Date() } = deps;
  const chip = syncChip(deps)(workspaceId);

  const panel = {
    open: false,
    init() {
      actions.ensure(this.workspaceId);
    },
    /** The id of the panel's dialog (`.popup`), the one its chip opens. */
    get dialogId() {
      return `sync-panel-${this.workspaceId}`;
    },
    /** The push question is showing in place of the Push button. */
    confirming: false,

    get view() {
      return this.gitState ? panelOf(this.gitState, { now: now(), busy: actions.busy(this.gitState) }) : null;
    },
    get expandedAttr() {
      return this.open ? "true" : "false";
    },
    get busyAttr() {
      return this.view?.busy ? "true" : null;
    },
    get commit() {
      return this.view?.commit ?? "";
    },
    get panelBranch() {
      return this.view?.branch ?? "";
    },
    get baseRef() {
      return this.view?.base.ref ?? "";
    },
    get baseHint() {
      return this.view?.base.hint ?? "";
    },
    get upstreamRef() {
      return this.view?.upstream.ref ?? "";
    },
    get hasUpstreamRef() {
      return !!this.view?.upstream.ref;
    },
    get upstreamHint() {
      return this.view?.upstream.hint ?? "";
    },
    get freshLine() {
      return this.view?.fresh.line ?? "";
    },
    /** data-old on the age line: over an hour, or never fetched. */
    get freshOldAttr() {
      return this.view?.fresh.old ? "" : null;
    },
    get fetchLabel() {
      return this.view?.fetchLabel ?? "";
    },
    get fetchDisabledAttr() {
      return this.view?.busy ? "true" : null;
    },
    get pushLabel() {
      return this.view?.pushLabel ?? "";
    },
    get pushDisabledAttr() {
      return this.view?.busy ? "true" : null;
    },
    /** The footer's Push shows when there is something to push and the question is not asked. */
    get showsPush() {
      return (this.view?.offersPush ?? false) && !this.confirming;
    },
    get showsQuestion() {
      return this.confirming && (this.view?.offersPush ?? false);
    },
    get question() {
      return this.view?.question ?? "";
    },

    // The last refusal: a push's, else the repository's fetch.
    get refusal() {
      const s = this.gitState;
      return s ? (store.pushFailure[s.workspaceId] ?? store.fetchFailure[s.repositoryId] ?? null) : null;
    },
    get hasRefusal() {
      return !!this.refusal;
    },
    /** The credential refusal is a banner; the others a danger strip with the code. */
    get refusalIsBanner() {
      return this.refusal?.tone === "banner";
    },
    get refusalIsStrip() {
      return this.refusal?.tone === "danger";
    },
    get refusalLead() {
      return this.refusal?.lead ?? "";
    },
    get refusalReason() {
      return this.refusal?.reason ?? "";
    },
    get refusalCode() {
      return this.refusal?.code ?? "";
    },
    get refusalHref() {
      return this.refusal?.href ?? "";
    },
    get refusalLinkText() {
      return this.refusal?.linkText ?? "";
    },

    /** The chip was pressed: the dialog (placement, focus trap, Escape and the phone's sheet are its) opens under it. */
    openPanel(event) {
      this.show();
      this.$dispatch("dialog-open", { id: this.dialogId, anchor: event.currentTarget });
    },
    /** The dialog closed (Escape, a press outside, or Close). */
    panelClosed(event) {
      if (event.detail?.id === this.dialogId) this.close();
    },
    get fetchedPhrase() {
      return fetchedPhrase(this.gitState?.fetchedAt ?? null, now(), this.gitState?.pushedAt ?? null);
    },

    toggle() {
      if (this.open) this.close();
      else this.show();
    },
    show() {
      this.open = true;
    },
    close() {
      this.open = false;
      this.confirming = false;
    },

    /** Fetches the repository's origin: the chip and the panel say Fetching… until the new counts are in. */
    async fetch() {
      if (!this.gitState) return;
      this.confirming = false;
      const ok = await actions.fetchRepository(this.gitState.repositoryId);
      this.say(ok ? "Fetched. Counts updated." : this.refusalLead);
    },
    /** Asks the push question in place; nothing is pushed yet. */
    askPush() {
      if (this.view?.offersPush) this.confirming = true;
    },
    cancelPush() {
      this.confirming = false;
    },
    /** Pushes at the head the person is looking at. */
    async confirmPush() {
      if (!this.confirming || !this.gitState) return;
      const ok = await actions.push(this.gitState.workspaceId);
      this.confirming = false;
      this.say(ok ? "Pushed." : this.refusalLead);
    },
    dismissRefusal() {
      const s = this.gitState;
      if (!s) return;
      const { [s.workspaceId]: _p, ...pushes } = store.pushFailure;
      const { [s.repositoryId]: _f, ...fetches } = store.fetchFailure;
      store.pushFailure = pushes;
      store.fetchFailure = fetches;
    },

    /** Says it in the page's polite live region. */
    say(text) {
      if (text) this.$dispatch("announce", { text });
    },
  };

  // Chip getters keep their getter-ness (and `this`) when merged.
  return Object.defineProperties(panel, Object.getOwnPropertyDescriptors(chip));
};
