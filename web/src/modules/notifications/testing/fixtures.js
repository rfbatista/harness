// Notifications for tests and web/dev pages.

export const N0 = new Date("2026-10-09T14:00:00Z");

/** @returns {import("../domain/notification.js").Notification} */
export function makeNotification(overrides = {}) {
  return Object.freeze({
    id: "n1",
    projectId: "p1",
    projectName: "harness",
    taskId: "t1",
    taskTitle: "Notification",
    sessionId: "s1",
    source: "system",
    kind: "review_requested",
    level: "action",
    title: "Review requested: Plan: Web UI — notifications",
    body: "The architect asks you to look at the plan.",
    link: "/projects/p1/tasks/t1",
    createdAt: N0,
    readAt: null,
    resolvedAt: null,
    ...overrides,
  });
}

/** The wire format of a notification. */
export function notificationDTO(n) {
  return {
    id: n.id,
    project_id: n.projectId,
    project_name: n.projectName,
    task_id: n.taskId || undefined,
    task_title: n.taskTitle || undefined,
    session_id: n.sessionId || undefined,
    source: n.source,
    kind: n.kind,
    level: n.level,
    title: n.title,
    body: n.body || undefined,
    link: n.link,
    created_at: n.createdAt.toISOString(),
    read_at: n.readAt?.toISOString(),
    resolved_at: n.resolvedAt?.toISOString(),
  };
}

/** n minutes after N0. */
export const at = (m) => new Date(N0.getTime() + m * 60_000);
