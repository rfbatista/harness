// RunGateway in memory, for tests and web/dev pages; it obeys the same
// contract as the real gateway (../testing/gateway-contract.js).

import { Codes, StructuredError } from "../../../shared/domain/errors.js";
import { toRunCommandsSeed } from "./dto.js";

/**
 * @param {{
 *   sessions?: string[],
 *   commands?: Record<string, { name: string, command: string }[]>,  repository id → saved commands
 *   repositoryOf?: Record<string, string>,  session id → repository id
 *   dirOf?: Record<string, string>,  session id → its worktree
 *   worktrees?: Record<string, import("../domain/run.js").RunWorktree[]>,  session id → the run worktrees on offer (default: only its own)
 * }} [world]
 */
export function memoryRuns({ sessions = [], commands = {}, repositoryOf = {}, dirOf = {}, worktrees = {} } = {}) {
  const known = new Set(sessions);
  const runs = [];
  let next = 1;
  let clock = Date.parse("2026-10-05T12:00:00Z");

  /** The run worktrees on offer to a session: the world's, or just its own. */
  const offered = (sessionId) =>
    (worktrees[sessionId] ?? [{ key: "own", kind: "own", branch: "", dir: dirOf[sessionId] ?? "", sessionId, agent: "", sessionStatus: "" }]).map((w) => ({ ...w }));

  /** @type {import("../domain/ports.js").RunGateway} */
  const gateway = {
    decodeCommandsSeed: toRunCommandsSeed,
    async listCommands(repositoryId) {
      return [...(commands[repositoryId] ?? [])].sort((a, b) => a.name.localeCompare(b.name));
    },
    async saveCommand(repositoryId, name, command) {
      name = name.trim();
      command = command.trim();
      if (!/^[A-Za-z0-9][A-Za-z0-9 ._-]{0,39}$/.test(name)) {
        throw new StructuredError(Codes.INVALID_NAME, "a run command needs a short name", 400);
      }
      if (!command) throw new StructuredError(Codes.INVALID_INPUT, "the command to run is required", 400);
      const list = (commands[repositoryId] ??= []);
      const saved = { name, command };
      const at = list.findIndex((c) => c.name === name);
      if (at >= 0) list[at] = saved;
      else list.push(saved);
      return { ...saved };
    },
    async deleteCommand(repositoryId, name) {
      const list = commands[repositoryId] ?? [];
      const at = list.findIndex((c) => c.name === name);
      if (at < 0) throw new StructuredError(Codes.RUN_COMMAND_NOT_FOUND, `no run command called ${name}`, 404);
      list.splice(at, 1);
    },
    async listRuns(sessionId) {
      return runs.filter((r) => r.sessionId === sessionId).sort((a, b) => b.startedAt - a.startedAt);
    },
    async listWorktrees(sessionId) {
      return offered(sessionId);
    },
    async start(sessionId, { name = "", command = "", worktree = "" }) {
      if (!known.has(sessionId)) throw new StructuredError(Codes.SESSION_NOT_FOUND, "session not found", 404);
      const picked = offered(sessionId).find((w) => w.key === (worktree || "own"));
      if (!picked) throw new StructuredError(Codes.WORKTREE_NOT_AVAILABLE, "that worktree is no longer available", 409);
      name = name.trim();
      command = command.trim();
      if (name) {
        const live = runs.find((r) => r.sessionId === sessionId && r.name === name && r.worktree.key === picked.key && r.status === "running");
        if (live) return live;
        const saved = (commands[repositoryOf[sessionId]] ?? []).find((c) => c.name === name);
        if (!saved) throw new StructuredError(Codes.RUN_COMMAND_NOT_FOUND, `no run command called ${name}`, 404);
        command = saved.command;
      } else if (!command) {
        throw new StructuredError(Codes.INVALID_INPUT, "pick a saved command or type one to run", 400);
      }
      clock += 1000;
      const run = { id: `run-${next++}`, sessionId, name: name || command, command, dir: picked.dir, worktree: { key: picked.key, kind: picked.kind, branch: picked.branch, sessionId: picked.sessionId }, status: "running", exitCode: 0, startedAt: new Date(clock) };
      runs.push(run);
      return { ...run };
    },
    async stop(runId) {
      const run = runs.find((r) => r.id === runId);
      if (!run) throw new StructuredError(Codes.RUN_NOT_FOUND, "run not found", 404);
      if (run.status === "running") run.status = "stopped";
      return { ...run };
    },
  };

  return {
    gateway,
    /** Takes a run worktree off offer, as when its directory is deleted. */
    dropWorktree(sessionId, key) {
      worktrees[sessionId] = offered(sessionId).filter((w) => w.key !== key);
    },
    /** Ends a run as its process would. */
    exit(runId, code) {
      const run = runs.find((r) => r.id === runId);
      if (run) Object.assign(run, { status: "exited", exitCode: code });
    },
  };
}
