// Review requests for tests and web/dev pages.

export const R0 = new Date("2026-10-02T14:00:00Z");

/** @returns {import("../domain/review.js").ReviewRequest} */
export function makeReview(overrides = {}) {
  return Object.freeze({
    id: "rv1",
    taskId: "t1",
    projectId: "p1",
    architectSessionId: "arch",
    aboutSessionId: "d1",
    subject: "Spec set ready for sign-off",
    body: "Three specs and two contracts. Please check the status authority rule.",
    documentIds: [],
    artifacts: [],
    state: "pending",
    responseNote: "",
    respondedAt: null,
    createdAt: R0,
    updatedAt: R0,
    ...overrides,
  });
}

/** The wire format of a review request. */
export function reviewDTO(r) {
  return {
    id: r.id,
    task_id: r.taskId,
    project_id: r.projectId,
    architect_session_id: r.architectSessionId,
    about_session_id: r.aboutSessionId || undefined,
    subject: r.subject,
    body: r.body,
    document_ids: r.documentIds,
    artifact_ids: r.artifacts.map((a) => a.id),
    state: r.state,
    response_note: r.responseNote || undefined,
    responded_at: r.respondedAt?.toISOString(),
    created_at: r.createdAt.toISOString(),
    updated_at: r.updatedAt.toISOString(),
  };
}
