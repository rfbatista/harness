// Domain notifications as the server sends them, for the fake server of the HTTP adapter's tests.

import type { Notification, NotificationPage } from "../domain/notification";

export function toWire(n: Notification): Record<string, unknown> {
  return {
    id: n.id,
    project_id: n.projectId,
    project_name: n.projectName,
    ...(n.taskId ? { task_id: n.taskId, task_title: n.taskTitle } : {}),
    ...(n.sessionId ? { session_id: n.sessionId } : {}),
    source: n.source,
    kind: n.kind,
    level: n.level,
    title: n.title,
    ...(n.body ? { body: n.body } : {}),
    link: n.link,
    created_at: n.createdAt.toISOString(),
    ...(n.readAt ? { read_at: n.readAt.toISOString() } : {}),
    ...(n.resolvedAt ? { resolved_at: n.resolvedAt.toISOString() } : {}),
    ...(n.interruptedSessions.length
      ? { interrupted_sessions: n.interruptedSessions.map((s) => ({ session_id: s.sessionId, project_id: s.projectId, task_id: s.taskId, task_title: s.taskTitle, agent_name: s.agentName, link: s.link, state: s.state, ...(s.reason ? { reason: s.reason } : {}) })) }
      : {}),
  };
}

export function pageToWire(page: NotificationPage): Record<string, unknown> {
  return { notifications: page.notifications.map(toWire), unread_count: page.unreadCount, ...(page.nextBefore ? { next_before: page.nextBefore } : {}) };
}
