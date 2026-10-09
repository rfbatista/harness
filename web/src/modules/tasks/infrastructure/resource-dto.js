// The wire format of Resource and Comment (Contract: Harness server ↔ Web UI
// — task resources and comments) and of their project-feed changes, mapped
// to domain values. The only place that knows the JSON shape.

import { Codes, StructuredError } from "../../../shared/domain/errors.js";
import { KINDS } from "../domain/resources.js";

function bad(detail) {
  throw new StructuredError(Codes.BAD_RESPONSE, `Unexpected resource data: ${detail}.`);
}

function dates(dto, what) {
  const updatedAt = new Date(dto.updated_at);
  const createdAt = new Date(dto.created_at ?? dto.updated_at);
  if (Number.isNaN(updatedAt.getTime()) || Number.isNaN(createdAt.getTime())) bad(`${what} ${dto.id} has an invalid date`);
  return { createdAt, updatedAt };
}

/** @returns {import("../domain/resources.js").Resource} */
export function toResource(dto) {
  if (!dto || typeof dto.id !== "string" || dto.id === "") bad("resource without an id");
  if (!KINDS.includes(dto.kind)) bad(`resource ${dto.id} has unknown kind "${dto.kind}"`);
  if (dto.kind === "link" && typeof dto.url !== "string") bad(`link ${dto.id} without a url`);
  if (dto.kind !== "link" && typeof dto.content_url !== "string") bad(`resource ${dto.id} without a content_url`);
  return Object.freeze({
    id: dto.id,
    ticketId: dto.ticket_id ?? "",
    projectId: dto.project_id ?? "",
    kind: dto.kind,
    title: dto.title ?? "",
    mime: dto.mime ?? "",
    sizeBytes: Number(dto.size_bytes ?? 0),
    url: dto.kind === "link" ? dto.url : "",
    contentUrl: dto.kind === "link" ? "" : dto.content_url,
    ...dates(dto, "resource"),
  });
}

/** @returns {import("../domain/comments.js").Comment} */
export function toComment(dto) {
  if (!dto || typeof dto.id !== "string" || dto.id === "") bad("comment without an id");
  if (typeof dto.body !== "string") bad(`comment ${dto.id} without a body`);
  return Object.freeze({
    id: dto.id,
    ticketId: dto.ticket_id ?? "",
    projectId: dto.project_id ?? "",
    author: dto.author ?? "person",
    body: dto.body,
    ...dates(dto, "comment"),
  });
}

export function toResourceList(body) {
  if (!Array.isArray(body?.resources)) bad("expected {resources: [...]}");
  return body.resources.map(toResource);
}

export function toCommentList(body) {
  if (!Array.isArray(body?.comments)) bad("expected {comments: [...]}");
  return body.comments.map(toComment);
}

/** The seed the task's resources page embeds: the two lists in the API's shapes. */
export function toResourcesSeed(seed) {
  if (!seed || typeof seed.ticket_id !== "string" || seed.ticket_id === "") bad("seed without a ticket_id");
  return {
    projectId: seed.project_id ?? "",
    ticketId: seed.ticket_id,
    resources: toResourceList({ resources: seed.resources ?? [] }),
    comments: toCommentList({ comments: seed.comments ?? [] }),
  };
}

function ref(dto, key) {
  const r = dto[key];
  if (typeof r?.id !== "string" || r.id === "") bad(`deleted ${key} without an id`);
  return { id: r.id, ticketId: r.ticket_id ?? "" };
}

/**
 * A project-feed message as a ResourceChange, or null when it is about
 * something else (sessions, artifacts, documents, a ticket that changed).
 * Throws BAD_RESPONSE on a resource or comment change it cannot read.
 * @returns {import("../domain/ports.js").ResourceChange | null}
 */
export function toResourceChange(dto) {
  if (!dto || typeof dto !== "object") return null;
  if ("ticket_resource" in dto) {
    return dto.deleted ? { kind: "resource-deleted", ...ref(dto, "ticket_resource") } : { kind: "resource", resource: toResource(dto.ticket_resource) };
  }
  if ("ticket_comment" in dto) {
    return dto.deleted ? { kind: "comment-deleted", ...ref(dto, "ticket_comment") } : { kind: "comment", comment: toComment(dto.ticket_comment) };
  }
  if ("ticket" in dto && dto.deleted) return { kind: "task-deleted", id: ref(dto, "ticket").id };
  return null;
}
