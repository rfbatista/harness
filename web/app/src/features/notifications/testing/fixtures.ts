import type { Notification } from "../domain/notification";

let sequence = 0;

/** A notification, unread by default, with the given fields. Each call is one minute older than the one before. */
export function notification(fields: Partial<Notification> = {}): Notification {
  sequence += 1;
  return {
    id: `n${sequence}`,
    projectId: "p1",
    projectName: "harness",
    taskId: "t1",
    taskTitle: "Ship the thing",
    sessionId: "",
    source: "system",
    kind: "task_review",
    level: "action",
    title: `Notification ${sequence}`,
    body: "",
    link: "/projects/p1/tasks/t1",
    createdAt: new Date(Date.UTC(2026, 10, 21, 12, 0, 0) - sequence * 60_000),
    readAt: null,
    resolvedAt: null,
    interruptedSessions: [],
    ...fields,
  };
}

export const read = (n: Notification): Notification => ({ ...n, readAt: new Date(Date.UTC(2026, 10, 21, 13, 0, 0)) });
