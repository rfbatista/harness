// Tasks for tests.

export function makeTask(overrides = {}) {
  return Object.freeze({
    id: "t1",
    projectId: "p1",
    title: "Add SSE feed",
    description: "Stream session changes to clients.",
    status: "in_progress",
    parentId: "",
    architectSessionId: "",
    pendingReviews: 0,
    updatedAt: 0,
    createdAt: 0,
    statusSince: 0,
    archivedAt: 0,
    lastMove: null,
    mergeState: null,
    ...overrides,
  });
}

/** The wire format of a task (ticket). */
export function taskDTO(t) {
  return {
    id: t.id,
    project_id: t.projectId,
    title: t.title,
    description: t.description || undefined,
    status: t.status,
    parent_id: t.parentId ?? "",
    architect_session_id: t.architectSessionId || null,
    pending_reviews: t.pendingReviews,
    updated_at: t.updatedAt ? new Date(t.updatedAt).toISOString() : undefined,
    created_at: iso(t.createdAt),
    status_since: iso(t.statusSince),
    archived_at: t.archivedAt ? iso(t.archivedAt) : null,
    last_move: t.lastMove
      ? { status: t.lastMove.status, by: t.lastMove.by, session_id: t.lastMove.sessionId || undefined, session_label: t.lastMove.sessionLabel || undefined, reason: t.lastMove.reason || undefined, at: iso(t.lastMove.at) }
      : null,
    merge_state: t.mergeState ? { ...t.mergeState } : null,
  };
}

const iso = (ms) => (ms ? new Date(ms).toISOString() : undefined);
