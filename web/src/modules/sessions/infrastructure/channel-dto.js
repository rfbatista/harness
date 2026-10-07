// The wire format of the architect channel's task messages (the Web UI
// contract: GET /api/task_messages and the project feed's `task_message`
// messages) and its mapping to domain messages. Status checks ride on the
// session (dto.js); a `status_check` feed message is always followed by the
// delegate's session change, which is what the page reads.

import { Codes, StructuredError } from "../../../shared/domain/errors.js";
import { KINDS, REPORT_STATUSES, VERDICTS } from "../domain/channel.js";
import { optionalDate } from "./dto.js";

/** @returns {import("../domain/channel.js").TaskMessage} */
export function toTaskMessage(dto) {
  if (!dto || typeof dto.id !== "string" || dto.id === "") bad("task message without an id");
  const at = `task message ${dto.id}`;
  if (!KINDS.includes(dto.kind)) bad(`${at} has unknown kind "${dto.kind}"`);
  const status = dto.status ?? "";
  if (status !== "" && !REPORT_STATUSES.includes(status)) bad(`${at} has unknown status "${status}"`);
  const verdict = dto.verdict ?? "";
  if (verdict !== "" && !VERDICTS.includes(verdict)) bad(`${at} has unknown verdict "${verdict}"`);
  const createdAt = optionalDate(dto.created_at, `${at}: created_at`);
  if (!createdAt) bad(`${at} has no created_at`);
  return Object.freeze({
    id: dto.id,
    taskId: dto.task_id ?? "",
    fromSessionId: dto.from_session_id ?? "",
    toSessionId: dto.to_session_id ?? "",
    kind: dto.kind,
    subject: dto.subject ?? "",
    body: dto.body ?? "",
    status,
    verdict,
    inReplyTo: dto.in_reply_to ?? "",
    documentIds: ids(dto.document_ids),
    artifactIds: ids(dto.artifact_ids),
    delivered: dto.delivered === true,
    deliveredAt: optionalDate(dto.delivered_at, `${at}: delivered_at`),
    createdAt,
  });
}

/** GET /api/task_messages → {"messages": [...]}, oldest first. */
export function toTaskMessages(body) {
  if (!body || !Array.isArray(body.messages)) bad("expected {messages: [...]}");
  return body.messages.map(toTaskMessage);
}

/**
 * One feed message as the channel sees it: a task message, or null for any
 * other kind (consumers ignore kinds they do not know). The project feed
 * keys it, {"task_message": {...}}; a session stream types it,
 * {"type": "task_message", "task_message": {...}}. Both read the same.
 * Throws BAD_RESPONSE when it is a task message that cannot be read.
 */
export function toChannelEvent(dto) {
  if (!dto || typeof dto !== "object" || !("task_message" in dto)) return null;
  if ("type" in dto && dto.type !== "task_message") return null;
  return { kind: "message", message: toTaskMessage(dto.task_message) };
}

const ids = (v) => (Array.isArray(v) ? v.filter((x) => typeof x === "string") : []);

function bad(detail) {
  throw new StructuredError(Codes.BAD_RESPONSE, `Unexpected task message: ${detail}.`);
}
