// The wire format of runs and run commands (internal/domain/apprun.go).

import { Codes, StructuredError } from "../../../shared/domain/errors.js";

const STATUSES = ["running", "exited", "stopped"];

/** @returns {import("../domain/run.js").Run} */
export function toRun(dto) {
  if (!dto || typeof dto.id !== "string" || dto.id === "") bad("run without an id");
  if (!STATUSES.includes(dto.status)) bad(`run ${dto.id} has unknown status "${dto.status}"`);
  return Object.freeze({
    id: dto.id,
    sessionId: dto.session_id ?? "",
    name: dto.name ?? "",
    command: dto.command ?? "",
    dir: dto.dir ?? "",
    worktree: toRunWorktreeRef(dto.worktree),
    status: dto.status,
    exitCode: Number(dto.exit_code ?? 0),
    startedAt: new Date(dto.started_at),
  });
}

/** What a run says of its run worktree; an older server says nothing: the session's own. */
function toRunWorktreeRef(dto) {
  return Object.freeze({
    key: dto?.key || "own",
    kind: dto?.kind || "own",
    branch: dto?.branch ?? "",
    sessionId: dto?.session_id ?? "",
  });
}

/** @returns {import("../domain/run.js").RunWorktree} */
export function toRunWorktree(dto) {
  if (!dto || typeof dto.key !== "string" || dto.key === "") bad("run worktree without a key");
  return Object.freeze({
    key: dto.key,
    kind: dto.kind ?? "session",
    branch: dto.branch ?? "",
    dir: dto.dir ?? "",
    sessionId: dto.session_id ?? "",
    agent: dto.agent ?? "",
    sessionStatus: dto.session_status ?? "",
  });
}

/** @returns {import("../domain/run.js").RunCommand} */
export function toRunCommand(dto) {
  if (!dto || typeof dto.name !== "string" || dto.name === "") bad("run command without a name");
  return Object.freeze({ name: dto.name, command: dto.command ?? "" });
}

/** A repository settings page's run commands seed (RunCommandsSeed in Go). */
export function toRunCommandsSeed(body) {
  if (!body || typeof body.repository_id !== "string" || !Array.isArray(body.run_commands)) {
    bad("expected {repository_id, run_commands: [...]}");
  }
  return { repositoryId: body.repository_id, commands: body.run_commands.map(toRunCommand) };
}

function bad(detail) {
  throw new StructuredError(Codes.BAD_RESPONSE, `Unexpected run data: ${detail}.`);
}
