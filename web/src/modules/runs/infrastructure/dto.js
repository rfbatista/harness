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
    status: dto.status,
    exitCode: Number(dto.exit_code ?? 0),
    startedAt: new Date(dto.started_at),
  });
}

/** @returns {import("../domain/run.js").RunCommand} */
export function toRunCommand(dto) {
  if (!dto || typeof dto.name !== "string" || dto.name === "") bad("run command without a name");
  return Object.freeze({ name: dto.name, command: dto.command ?? "" });
}

function bad(detail) {
  throw new StructuredError(Codes.BAD_RESPONSE, `Unexpected run data: ${detail}.`);
}
