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
    parentId: dto.parent_id ?? "",
    architectSessionId: dto.architect_session_id ?? "",
    pendingReviews: Number(dto.pending_reviews ?? 0),
    updatedAt: Date.parse(dto.updated_at ?? "") || 0,
    createdAt: time(dto.created_at),
    // A task never moved entered its column when it was created.
    statusSince: time(dto.status_since) || time(dto.created_at),
    archivedAt: time(dto.archived_at),
    lastMove: toLastMove(dto.last_move),
    mergeState: toMergeState(dto.merge_state),
  });
}

/** ms since the epoch; 0 when absent, unreadable or the server's zero time (year 1). */
function time(s) {
  const ms = Date.parse(s ?? "");
  return ms > 0 ? ms : 0;
}

/** Old events carry no last move; a malformed one is left out rather than failing the task. */
function toLastMove(m) {
  if (!m || typeof m.by !== "string" || !isStatus(m.status)) return null;
  return Object.freeze({ status: m.status, by: m.by, sessionId: m.session_id ?? "", sessionLabel: m.session_label ?? "", reason: m.reason ?? "", at: time(m.at) });
}

function toMergeState(m) {
  if (!m || typeof m.state !== "string") return null;
  return Object.freeze({ state: m.state, merged: Number(m.merged ?? 0), total: Number(m.total ?? 0) });
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
