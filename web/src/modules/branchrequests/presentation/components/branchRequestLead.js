// A session row's "N approvals pending" text on the task page. When one of the
// approvals is a branch request it leads to that request: it asks the Branch
// requests popover to open at the session's oldest pending one (ADR 0079). A
// session waiting only on claude's tool prompt keeps plain text.
//
//   <span x-data="branchRequestLead(row.id)" x-text="row.detail.lead"
//         x-bind:role="role(row.detail.kind)" x-on:click="open(row.detail.kind, $event)"> … </span>

/** The row's detail kind of "approvals pending" (view.js detailOf). */
const APPROVAL = "approval";

/** @param {{ store: { pendingBySession: Record<string, string> } }} deps */
export const branchRequestLead = ({ store }) => (sessionId = "") => ({
  actionable(kind) {
    return kind === APPROVAL && Boolean(store.pendingBySession[sessionId]);
  },
  role(kind) {
    return this.actionable(kind) ? "button" : null;
  },
  tabindex(kind) {
    return this.actionable(kind) ? "0" : null;
  },
  actionAttr(kind) {
    return this.actionable(kind) ? "" : null;
  },
  describe(kind) {
    return this.actionable(kind) ? "Show the branch request waiting on you" : null;
  },

  /** Asks the Branch requests popover to open at this session's request; the click is not a row selection. */
  open(kind, event) {
    if (!this.actionable(kind)) return;
    event?.stopPropagation?.();
    event?.preventDefault?.();
    this.$dispatch("branch-request-show", { sessionId });
  },
});
