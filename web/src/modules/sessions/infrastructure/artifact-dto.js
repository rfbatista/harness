// The wire format of the Artifact resource and the `artifact` session event
// (Contract: Server ↔ Web UI — Artifacts), mapped to domain artifacts. The
// only place that knows the JSON shape and the view route.

import { Codes, StructuredError } from "../../../shared/domain/errors.js";
import { KINDS, SCOPES, Scope } from "../domain/artifact.js";

/** Where the browser loads an artifact's bytes: GET /api/artifacts/:id/view/ (trailing slash: relative references resolve under it). */
export const viewPath = (id, base = "/api") => `${base}/artifacts/${encodeURIComponent(id)}/view/`;

/** @returns {import("../domain/artifact.js").Artifact} */
export function toArtifact(dto, base = "/api") {
  if (!dto || typeof dto.id !== "string" || dto.id === "") bad("artifact without an id");
  if (!KINDS.includes(dto.kind)) bad(`artifact ${dto.id} has unknown kind "${dto.kind}"`);
  const revision = Number(dto.revision);
  if (!Number.isInteger(revision) || revision < 1) bad(`artifact ${dto.id} has an invalid revision`);
  const updatedAt = new Date(dto.updated_at);
  const createdAt = new Date(dto.created_at ?? dto.updated_at);
  if (Number.isNaN(updatedAt.getTime()) || Number.isNaN(createdAt.getTime())) bad(`artifact ${dto.id} has an invalid date`);
  if (dto.kind === "url" && typeof dto.url !== "string") bad(`artifact ${dto.id} is a url without one`);
  const url = dto.kind === "url" ? dto.url : "";
  // A server from before scopes does not send it: every artifact was a task's then.
  const scope = dto.scope ?? Scope.TASK;
  if (!SCOPES.includes(scope)) bad(`artifact ${dto.id} has unknown scope "${scope}"`);
  const attachedTicketIds = toTicketIds(dto.attached_ticket_ids, dto.id);

  return Object.freeze({
    id: dto.id,
    sessionId: dto.session_id ?? "",
    ticketId: dto.ticket_id ?? "",
    projectId: dto.project_id ?? "",
    kind: dto.kind,
    title: dto.title ?? "",
    note: dto.note ?? "",
    path: dto.kind === "url" ? "" : (dto.path ?? ""),
    url,
    mime: dto.mime ?? "",
    sizeBytes: Number(dto.size_bytes ?? 0),
    revision,
    scope,
    attachedTicketIds,
    createdAt,
    updatedAt,
    src: dto.kind === "url" ? url : viewPath(dto.id, base),
  });
}

/** GET /api/artifacts → {"artifacts": [...]} */
export function toArtifactList(body, base = "/api") {
  if (!body || !Array.isArray(body.artifacts)) bad("expected {artifacts: [...]}");
  return body.artifacts.map((dto) => toArtifact(dto, base));
}

/** POST /api/set_artifact_scope, /api/attach_artifact_to_ticket and /api/detach_artifact_from_ticket → {"artifact": {...}} */
export function toArtifactAnswer(body, base = "/api") {
  if (!body?.artifact) bad("expected {artifact: {...}}");
  return toArtifact(body.artifact, base);
}

/**
 * One `data:` line of GET /api/events (the project feed).
 * @returns {import("../domain/ports.js").ProjectArtifactChange | null} null for the feed's other keys
 */
export function toProjectArtifactChange(dto, base = "/api") {
  if (!dto || typeof dto !== "object" || !("artifact" in dto)) return null;
  if (!dto.deleted) return { kind: "changed", artifact: toArtifact(dto.artifact, base) };
  const a = dto.artifact;
  if (typeof a?.id !== "string" || a.id === "") bad("deleted change without an id");
  return {
    kind: "deleted",
    id: a.id,
    projectId: a.project_id ?? "",
    ticketId: a.ticket_id ?? "",
    attachedTicketIds: toTicketIds(a.attached_ticket_ids, a.id),
  };
}

/**
 * One `data:` line of GET /api/sessions/:id/events (a ports.SessionEvent).
 * @returns {import("../domain/ports.js").ArtifactEvent | null} null for the event types the Design tab ignores
 */
export function toArtifactEvent(dto, base = "/api") {
  if (dto?.type === "artifact") return { kind: "published", artifact: toArtifact(dto.artifact, base) };
  if (dto?.type === "done") return { kind: "ended" };
  return null;
}

/** attached_ticket_ids: a server from before attachments does not send it, and nothing was attached then. */
function toTicketIds(ids, artifactId) {
  if (ids === undefined || ids === null) return Object.freeze([]);
  if (!Array.isArray(ids) || ids.some((id) => typeof id !== "string" || id === "")) bad(`artifact ${artifactId} has malformed attached_ticket_ids`);
  return Object.freeze([...ids]);
}

function bad(detail) {
  throw new StructuredError(Codes.BAD_RESPONSE, `Unexpected artifact data: ${detail}.`);
}
