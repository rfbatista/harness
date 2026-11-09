// A session row's "N approvals pending" text on the task page. When one of the
// approvals is a branch request it leads to that request: it asks the Branch
// requests popover to open at the session's oldest pending one (ADR 0079). A
// session waiting only on claude's tool prompt keeps plain text.
//
//   <span x-data="branchRequestLead(row.id)">
//     <button x-show="actionable(row.detail.kind)" x-on:click.stop="open(row.detail.kind)"> … </button>
//     <span x-show="plain(row.detail.kind)"> … </span>
//   </span>

/** The row's detail kind of "approvals pending" (view.js detailOf). */
const APPROVAL = "approval";

/** @param {{ store: { pendingBySession: Record<string, string> } }} deps */
export const branchRequestLead = ({ store }) => (sessionId = "") => ({
  actionable(kind) {
    return kind === APPROVAL && Boolean(store.pendingBySession[sessionId]);
  },
  /** The plain text of every other row, and of a session waiting only on a tool prompt. */
  plain(kind) {
    return !this.actionable(kind);
  },
  describe(kind, lead = "") {
    return `${lead}: show the branch request waiting on you`;
  },

  /** Asks the Branch requests popover to open at this session's request; . */
  open(kind) {
    if (!this.actionable(kind)) return;
    this.$dispatch("branch-request-show", { sessionId });
  },
});
