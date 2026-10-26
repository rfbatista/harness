// RunGateway over the harness HTTP API.

import { toRun, toRunCommand, toRunCommandsSeed, toRunWorktree } from "./dto.js";

/**
 * @param {import("../../../shared/infrastructure/api.js").ApiClient} api
 * @returns {import("../domain/ports.js").RunGateway}
 */
export function runsGateway(api) {
  return {
    decodeCommandsSeed: toRunCommandsSeed,
    async listCommands(repositoryId) {
      const body = await api.get("/list_repository_run_commands", { repository_id: repositoryId });
      return (body?.run_commands ?? []).map(toRunCommand);
    },
    async saveCommand(repositoryId, name, command) {
      const body = await api.post("/save_repository_run_command", { repository_id: repositoryId, name, command });
      return toRunCommand(body?.run_command);
    },
    async deleteCommand(repositoryId, name) {
      await api.post("/delete_repository_run_command", { repository_id: repositoryId, name });
    },
    async listRuns(sessionId) {
      const body = await api.get("/list_runs", { session_id: sessionId });
      return (body?.runs ?? []).map(toRun);
    },
    async listWorktrees(sessionId) {
      const body = await api.get("/list_run_worktrees", { session_id: sessionId });
      return (body?.worktrees ?? []).map(toRunWorktree);
    },
    async start(sessionId, { name = "", command = "", worktree = "" }) {
      const body = await api.post("/start_run", { session_id: sessionId, name, command, worktree });
      return toRun(body?.run);
    },
    async stop(runId) {
      const body = await api.post("/stop_run", { run_id: runId });
      return toRun(body?.run);
    },
  };
}
