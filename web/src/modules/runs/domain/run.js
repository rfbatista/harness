// Application runs: a repository's app run from a session's worktree, in a
// terminal on the server. Mirrors internal/domain/apprun.go.

/**
 * @typedef {"running"|"exited"|"stopped"} RunStatus
 *
 * @typedef {object} Run
 * @property {string} id
 * @property {string} sessionId
 * @property {string} name      the saved command's name, or the command line run ad hoc
 * @property {string} command
 * @property {string} dir       the worktree it runs in
 * @property {RunWorktreeRef} worktree  which run worktree that is
 * @property {RunStatus} status
 * @property {number} exitCode
 * @property {Date} startedAt
 *
 * @typedef {object} RunWorktreeRef  how a run names its run worktree
 * @property {string} key
 * @property {"own"|"main"|"session"} kind
 * @property {string} branch
 * @property {string} sessionId
 *
 * @typedef {object} RunWorktree  a directory a session can run in (the server's order is the display order)
 * @property {string} key        opaque: "own", "main" or "session:<id>"
 * @property {"own"|"main"|"session"} kind
 * @property {string} branch     "" when detached
 * @property {string} dir
 * @property {string} sessionId
 * @property {string} agent      session kind only; "" for none
 * @property {string} sessionStatus  session kind only
 *
 * @typedef {object} RunCommand  a saved way to run the repository's app
 * @property {string} name
 * @property {string} command
 */

export const isRunning = (run) => run.status === "running";

/** How a run's state reads, and its .status block state. */
export function runState(run) {
  switch (run.status) {
    case "running":
      return { state: "running", word: "running" };
    case "stopped":
      return { state: "done", word: "stopped" };
    default:
      return run.exitCode === 0
        ? { state: "done", word: "exited" }
        : { state: "failed", word: `exited (code ${run.exitCode})` };
  }
}

/** The run to show first: the newest running one, else the newest. */
export function preferredRun(runs) {
  return runs.find(isRunning) ?? runs[0] ?? null;
}

/** The longest name a run command takes (internal/domain/apprun.go). */
export const MAX_RUN_COMMAND_NAME = 40;

/**
 * Why a run command's name is refused, or "" when it is fine. The rule is
 * the server's (domain.CleanRunCommand): after trimming, 1 to 40 letters,
 * digits, spaces and . _ -, starting with a letter or digit.
 */
export function runCommandNameProblem(name) {
  const n = name.trim();
  if (n === "") return "A run command needs a name.";
  if (n.length > MAX_RUN_COMMAND_NAME) return `Up to ${MAX_RUN_COMMAND_NAME} characters; this one has ${n.length}.`;
  if (!/^[A-Za-z0-9 ._-]*$/.test(n)) return "Use only letters, digits, spaces and . _ -";
  if (!/^[A-Za-z0-9]/.test(n)) return "Start with a letter or a digit.";
  return "";
}

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
