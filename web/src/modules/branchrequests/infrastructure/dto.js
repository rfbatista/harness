// The wire format of branch requests (GET /api/branch_requests,
// POST /api/branch_requests/{id}/answer and the project feed's
// `branch_request` messages) and its mapping to domain requests.

import { Codes, StructuredError } from "../../../shared/domain/errors.js";
import { STATES } from "../domain/request.js";

/** @returns {import("../domain/request.js").BranchRequest} */
export function toRequest(dto) {
  if (!dto || typeof dto.id !== "string" || dto.id === "") bad("branch request without an id");
  const at = `branch request ${dto.id}`;
  if (!STATES.includes(dto.state)) bad(`${at} has unknown state "${dto.state}"`);
  const requestedAt = new Date(dto.requested_at);
  if (Number.isNaN(requestedAt.getTime())) bad(`${at} has no requested_at`);
  const start = dto.start_point;
  const remote = dto.remote;
  return Object.freeze({
    id: dto.id,
    sessionId: dto.session_id ?? "",
    taskId: dto.task_id ?? "",
    repositoryId: dto.repository_id ?? "",
    sessionBranch: dto.session_branch ?? "",
    action: dto.action ?? "",
    branch: dto.branch ?? "",
    newName: dto.new_name ?? "",
    startPoint: start ? Object.freeze({ kind: start.kind ?? "", ref: start.ref ?? "", commit: start.commit ?? "" }) : null,
    remote: remote ? Object.freeze({ name: remote.name ?? "", url: remote.url ?? "", newName: remote.new_name ?? "" }) : null,
    remoteBranch: dto.remote_branch ?? "",
    commits: typeof dto.commits === "number" ? dto.commits : null,
    commitsAgainst: dto.commits_against ?? "",
    via: dto.via ?? "git",
    reason: dto.reason ?? "",
    state: dto.state,
    note: dto.note ?? "",
    requestedAt,
  });
}

/** GET /api/branch_requests → {"requests": [...]}, newest first. */
export function toRequests(body) {
  if (!body || !Array.isArray(body.requests)) bad("expected {requests: [...]}");
  return body.requests.map(toRequest);
}

/** POST /api/branch_requests/{id}/answer → {"request": {...}} */
export function toAnswer(body) {
  if (!body?.request) bad("expected {request: {...}}");
  return toRequest(body.request);
}

/**
 * One feed message as the branch requests see it, or null for any other kind.
 * The project feed keys it, {"branch_request": {...}}. Throws BAD_RESPONSE when
 * it is a branch request that cannot be read.
 */
export function toRequestEvent(dto) {
  if (!dto || typeof dto !== "object" || !("branch_request" in dto)) return null;
  return { request: toRequest(dto.branch_request) };
}

function bad(detail) {
  throw new StructuredError(Codes.BAD_RESPONSE, `Unexpected branch request: ${detail}.`);
}
