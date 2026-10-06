// The wire format of internal/domain/ticket.go and its mapping.

import { Codes, StructuredError } from "../../../shared/domain/errors.js";
import { isStatus } from "../domain/task.js";

/** @returns {import("../domain/task.js").Task} */
export function toTask(dto) {
  if (!dto || typeof dto.id !== "string" || dto.id === "") bad("task without an id");
  if (!isStatus(dto.status)) bad(`task ${dto.id} has unknown status "${dto.status}"`);
  return Object.freeze({
    id: dto.id,
    projectId: dto.project_id ?? "",
    title: dto.title ?? "",
    description: dto.description ?? "",
    status: dto.status,
  });
}

/**
 * The tasks of a list that can be read; one the client cannot read (a status
 * the server knows and this client does not) is left out rather than failing
 * the whole list, so the rail and the board keep working for the rest.
 * @returns {import("../domain/task.js").Task[]}
 */
export function toTasks(list) {
  return list.flatMap((dto) => {
    try {
      return [toTask(dto)];
    } catch {
      return [];
    }
  });
}

function bad(detail) {
  throw new StructuredError(Codes.BAD_RESPONSE, `Unexpected task data: ${detail}.`);
}
