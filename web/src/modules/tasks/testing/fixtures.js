// Tasks for tests.

export function makeTask(overrides = {}) {
  return Object.freeze({
    id: "t1",
    projectId: "p1",
    title: "Add SSE feed",
    description: "Stream session changes to clients.",
    status: "in_progress",
    ...overrides,
  });
}

/** The wire format of a task (ticket). */
export function taskDTO(t) {
  return { id: t.id, project_id: t.projectId, title: t.title, description: t.description || undefined, status: t.status };
}
