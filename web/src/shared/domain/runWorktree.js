// The words a run worktree goes by: the "Run in" picker's options, trigger and
// badges. Shared by the App tab (runs module) and the Services page, which
// offer the same run worktrees (ADR 0039, ADR 0077).

/** The key of the session's own worktree: what a run uses when none is picked. */
export const OWN_WORKTREE = "own";

const KIND_WORD = { own: "This session", main: "Main checkout" };

/** Who a run worktree belongs to, in words: "This session", "Main checkout", the agent, or "Session". */
export function worktreeKindWord(w) {
  return KIND_WORD[w.kind] ?? (w.agent || "Session");
}

/** A branch as shown; a detached HEAD has none. */
export const branchLabel = (branch) => branch || "detached HEAD";

/** What a session's status reads as in a worktree option: ended sessions say so. */
export function sessionStatusWord(status) {
  return ["done", "failed", "stopped"].includes(status) ? "ended" : (status || "ended").replaceAll("_", " ");
}

/** An option's second line in the picker. */
export function worktreeDescription(w) {
  if (w.kind === "own") return "This session's worktree";
  if (w.kind === "main") return "Main checkout · your own working copy";
  return `${w.agent || "no agent"} · ${sessionStatusWord(w.sessionStatus)}`;
}

/** The status dot's tone for a session option ("" for none). */
export function worktreeTone(w) {
  if (w.kind !== "session") return "";
  if (["running", "idle", "thinking", "starting"].includes(w.sessionStatus)) return "signal";
  if (w.sessionStatus === "waiting_approval") return "attention";
  if (w.sessionStatus === "failed") return "danger";
  return "";
}

/** The trigger's and a run badge's words: "<kind word> · <branch>". */
export function worktreeBadge(w) {
  return `${worktreeKindWord(w)} · ${branchLabel(w.branch)}`;
}
