// Artifacts for tests and web/dev pages, and their wire format.

export const A0 = new Date("2026-10-05T12:00:00Z");

/** @returns {import("../domain/artifact.js").Artifact} */
export function makeArtifact(overrides = {}) {
  const base = {
    id: "a1",
    sessionId: "s1",
    ticketId: "t1",
    projectId: "p1",
    kind: "page",
    title: "Pricing card",
    note: "First cut",
    path: "design/pricing.html",
    url: "",
    mime: "text/html",
    sizeBytes: 2048,
    revision: 1,
    createdAt: A0,
    updatedAt: A0,
  };
  const a = { ...base, ...overrides };
  return Object.freeze({ ...a, src: overrides.src ?? (a.kind === "url" ? a.url : `/api/artifacts/${encodeURIComponent(a.id)}/view/`) });
}

/** The wire format of an artifact (Contract: Server ↔ Web UI — Artifacts). */
export function toArtifactDTO(a) {
  return {
    id: a.id,
    session_id: a.sessionId,
    ticket_id: a.ticketId,
    project_id: a.projectId,
    kind: a.kind,
    title: a.title,
    note: a.note,
    path: a.kind === "url" ? null : a.path,
    url: a.kind === "url" ? a.url : null,
    mime: a.mime,
    size_bytes: a.sizeBytes,
    revision: a.revision,
    created_at: a.createdAt.toISOString(),
    updated_at: a.updatedAt.toISOString(),
  };
}
