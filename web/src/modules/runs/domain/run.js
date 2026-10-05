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
