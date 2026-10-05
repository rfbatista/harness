// Sessions for tests and web/dev pages.

export const T0 = new Date("2026-10-02T14:00:00Z");

/** @returns {import("../domain/session.js").Session} */
export function makeSession(overrides = {}) {
  return Object.freeze({
    id: "s1",
    projectId: "p1",
    task: "Port tickets screen to httpclient",
    agentId: "backend",
    status: "running",
    pendingApprovals: 0,
    lastAction: "",
    updatedAt: T0,
    ...overrides,
  });
}

/** The wire format of a session, as GET /api/sessions returns it. */
export function toDTO(session) {
  return {
    id: session.id,
    project_id: session.projectId,
    task: session.task,
    agent_id: session.agentId || undefined,
    status: session.status,
    pending_approvals: session.pendingApprovals,
    last_action: session.lastAction || undefined,
    updated_at: session.updatedAt.toISOString(),
  };
}
