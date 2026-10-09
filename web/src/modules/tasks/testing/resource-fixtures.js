// Resources and comments for tests and dev pages: one of every kind, and the
// wire shapes the server sends.

const AT = new Date("2026-10-08T12:00:00Z");

/** @returns {import("../domain/resources.js").Resource} */
export function makeResource(overrides = {}) {
  const id = overrides.id ?? "r1";
  const kind = overrides.kind ?? "image";
  return Object.freeze({
    id,
    ticketId: "t1",
    projectId: "p1",
    kind,
    title: "wireframe.png",
    mime: kind === "link" ? "" : "image/png",
    sizeBytes: kind === "link" ? 0 : 412_672,
    url: "",
    contentUrl: kind === "link" ? "" : `/api/ticket_resources/${id}/content`,
    createdAt: AT,
    updatedAt: AT,
    ...overrides,
  });
}

/** @returns {import("../domain/comments.js").Comment} */
export function makeComment(overrides = {}) {
  return Object.freeze({
    id: "c1",
    ticketId: "t1",
    projectId: "p1",
    author: "person",
    sessionId: "",
    agentName: "",
    body: "Keep the v2 contract.",
    createdAt: AT,
    updatedAt: AT,
    ...overrides,
  });
}

/** A task's resources, one of every kind the page treats differently, oldest first. */
export function everyKind(ticketId = "t1") {
  const at = (min) => new Date(AT.getTime() + min * 60_000);
  const r = (o) => makeResource({ ticketId, ...o, createdAt: at(o.n), updatedAt: at(o.n) });
  return [
    r({ n: 0, id: "r-png", kind: "image", title: "wireframe.png", mime: "image/png", sizeBytes: 412_672 }),
    r({ n: 1, id: "r-svg", kind: "image", title: "logo.svg", mime: "image/svg+xml", sizeBytes: 6_144 }),
    r({ n: 2, id: "r-pdf", kind: "file", title: "contract-v2.pdf", mime: "application/pdf", sizeBytes: 236_544 }),
    r({ n: 3, id: "r-txt", kind: "file", title: "notes.txt", mime: "text/plain; charset=utf-8", sizeBytes: 900 }),
    r({ n: 4, id: "r-json", kind: "file", title: "session-export.json", mime: "application/json", sizeBytes: 48_128 }),
    r({ n: 5, id: "r-m4a", kind: "audio", title: "standup.m4a", mime: "audio/mp4", sizeBytes: 1_887_437 }),
    r({ n: 6, id: "r-link", kind: "link", title: "Figma file", url: "https://figma.com/file/abc", mime: "", sizeBytes: 0, contentUrl: "" }),
  ];
}

/** A comment a session wrote: by default the Reviewer agent's session s-rev. */
export const sessionComment = (overrides = {}) => makeComment({ id: "c-agent", author: "session", sessionId: "s-rev", agentName: "Reviewer", body: "The contract tests pass on my branch.", ...overrides });

export function resourceDTO(r) {
  return {
    id: r.id,
    ticket_id: r.ticketId,
    project_id: r.projectId,
    kind: r.kind,
    title: r.title,
    mime: r.mime,
    size_bytes: r.sizeBytes,
    url: r.url,
    content_url: r.contentUrl,
    created_at: r.createdAt.toISOString(),
    updated_at: r.updatedAt.toISOString(),
  };
}

export function commentDTO(c) {
  return {
    id: c.id,
    ticket_id: c.ticketId,
    project_id: c.projectId,
    author: c.author,
    session_id: c.sessionId,
    agent_name: c.agentName,
    body: c.body,
    created_at: c.createdAt.toISOString(),
    updated_at: c.updatedAt.toISOString(),
  };
}
