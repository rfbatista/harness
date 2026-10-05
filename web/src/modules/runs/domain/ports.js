// What the runs module needs from the outside world. Implemented by
// infrastructure/runs-gateway.js (over /api) and
// infrastructure/memory-gateway.js; both run testing/gateway-contract.js.

/**
 * @typedef {import("./run.js").Run} Run
 * @typedef {import("./run.js").RunCommand} RunCommand
 *
 * @typedef {object} RunGateway
 * @property {(repositoryId: string) => Promise<RunCommand[]>} listCommands
 *           The repository's saved run commands, by name.
 * @property {(repositoryId: string, name: string, command: string) => Promise<RunCommand>} saveCommand
 *           Creates or replaces the saved command called name. Rejects with
 *           INVALID_NAME or INVALID_INPUT.
 * @property {(repositoryId: string, name: string) => Promise<void>} deleteCommand
 *           Rejects with RUN_COMMAND_NOT_FOUND.
 * @property {(sessionId: string) => Promise<Run[]>} listRuns
 *           The session's runs, newest first.
 * @property {(sessionId: string, what: { name?: string, command?: string }) => Promise<Run>} start
 *           Runs a saved command (name) or a command line (command) in the
 *           session's worktree. A saved command already running returns that
 *           run. Rejects with RUN_COMMAND_NOT_FOUND, INVALID_INPUT or
 *           SESSION_NOT_FOUND.
 * @property {(runId: string) => Promise<Run>} stop
 *           Rejects with RUN_NOT_FOUND.
 */

export {};
