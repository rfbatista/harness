// Tasks and sessions for the rail's tests: the domain values, and the wire shapes the server sends them in.

import type { RailSession } from "../domain/activity";
import type { Task } from "../domain/task";

export const task = (over: Partial<Task> = {}): Task => ({
  id: "t1",
  projectId: "p1",
  title: "Add SSE feed",
  status: "in_progress",
  parentId: "",
  pendingReviews: 0,
  updatedAt: 0,
  statusSince: 0,
  archivedAt: 0,
  mergeState: null,
  ...over,
});

export const session = (id: string, ticketId: string, status: string, over: Partial<RailSession> = {}): RailSession => ({ id, ticketId, status, pendingApprovals: 0, openInterviewCount: 0, ...over });

/** A task as the server's JSON carries it. */
export const taskWire = (t: Task): Record<string, unknown> => ({
  id: t.id,
  project_id: t.projectId,
  title: t.title,
  status: t.status,
  parent_id: t.parentId,
  pending_reviews: t.pendingReviews,
  created_at: "2026-01-01T00:00:00Z",
  updated_at: t.updatedAt ? new Date(t.updatedAt).toISOString() : "0001-01-01T00:00:00Z",
  status_since: t.statusSince ? new Date(t.statusSince).toISOString() : "0001-01-01T00:00:00Z",
  archived_at: t.archivedAt ? new Date(t.archivedAt).toISOString() : null,
  ...(t.mergeState ? { merge_state: { state: t.mergeState.state, merged: t.mergeState.merged, total: t.mergeState.total } } : {}),
});

export const sessionWire = (s: RailSession): Record<string, unknown> => ({ id: s.id, ticket_id: s.ticketId, status: s.status, pending_approvals: s.pendingApprovals, open_interview_count: s.openInterviewCount });
