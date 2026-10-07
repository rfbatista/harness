// The wire format of review requests (the Web UI contract:
// GET /api/review_requests, POST /api/respond_review_request and the project
// feed's `review_request` messages) and its mapping to domain requests.

import { Codes, StructuredError } from "../../../shared/domain/errors.js";
import { STATES } from "../domain/review.js";

/** Where the browser opens an artifact: its sandboxed view, as the Design tab does. */
const viewPath = (id, base) => `${base}/artifacts/${encodeURIComponent(id)}/view/`;

/** @returns {import("../domain/review.js").ReviewRequest} */
export function toReview(dto, base = "/api") {
  if (!dto || typeof dto.id !== "string" || dto.id === "") bad("review request without an id");
  const at = `review request ${dto.id}`;
  if (!STATES.includes(dto.state)) bad(`${at} has unknown state "${dto.state}"`);
  const createdAt = date(dto.created_at, `${at}: created_at`);
  if (!createdAt) bad(`${at} has no created_at`);
  return Object.freeze({
    id: dto.id,
    taskId: dto.task_id ?? "",
    projectId: dto.project_id ?? "",
    architectSessionId: dto.architect_session_id ?? "",
    aboutSessionId: dto.about_session_id ?? "",
    subject: dto.subject ?? "",
    body: dto.body ?? "",
    documentIds: ids(dto.document_ids),
    artifacts: ids(dto.artifact_ids).map((id) => ({ id, href: viewPath(id, base) })),
    state: dto.state,
    responseNote: dto.response_note ?? "",
    respondedAt: date(dto.responded_at, `${at}: responded_at`),
    createdAt,
    updatedAt: date(dto.updated_at, `${at}: updated_at`) ?? createdAt,
  });
}

/** GET /api/review_requests → {"review_requests": [...]}, newest first. */
export function toReviews(body, base) {
  if (!body || !Array.isArray(body.review_requests)) bad("expected {review_requests: [...]}");
  return body.review_requests.map((r) => toReview(r, base));
}

/** POST /api/respond_review_request → {"review_request": {...}, "delivered": bool} */
export function toResponse(body, base) {
  if (!body?.review_request) bad("expected {review_request: {...}, delivered}");
  return { review: toReview(body.review_request, base), delivered: body.delivered === true };
}

/**
 * One feed message as the reviews see it, or null for any other kind. The
 * project feed keys it, {"review_request": {...}}; a session stream types
 * it, {"type": "review_request", "review_request": {...}}. Throws
 * BAD_RESPONSE when it is a review request that cannot be read.
 */
export function toReviewEvent(dto, base) {
  if (!dto || typeof dto !== "object" || !("review_request" in dto)) return null;
  if ("type" in dto && dto.type !== "review_request") return null;
  return { kind: "review", review: toReview(dto.review_request, base) };
}

const ids = (v) => (Array.isArray(v) ? v.filter((x) => typeof x === "string") : []);

function date(value, what) {
  if (value === undefined || value === null || value === "") return null;
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) bad(`${what} is not a time`);
  return d;
}

function bad(detail) {
  throw new StructuredError(Codes.BAD_RESPONSE, `Unexpected review request: ${detail}.`);
}
