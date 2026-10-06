// The wire format of the Artifact resource and the `artifact` session event
// (Contract: Server ↔ Web UI — Artifacts), mapped to domain artifacts. The
// only place that knows the JSON shape and the view route.

import { Codes, StructuredError } from "../../../shared/domain/errors.js";
import { KINDS } from "../domain/artifact.js";

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

/**
 * One `data:` line of GET /api/sessions/:id/events (a ports.SessionEvent).
 * @returns {import("../domain/ports.js").ArtifactEvent | null} null for the event types the Design tab ignores
 */
export function toArtifactEvent(dto, base = "/api") {
  if (dto?.type === "artifact") return { kind: "published", artifact: toArtifact(dto.artifact, base) };
  if (dto?.type === "done") return { kind: "ended" };
  return null;
}

function bad(detail) {
  throw new StructuredError(Codes.BAD_RESPONSE, `Unexpected artifact data: ${detail}.`);
}
