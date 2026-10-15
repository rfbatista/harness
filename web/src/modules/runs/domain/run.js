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
 * @property {RunStatus} status
 * @property {number} exitCode
 * @property {Date} startedAt
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
