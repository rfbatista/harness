// A session of the task as the whiteboard draws it: only what a card needs.

export const STATUSES = ["starting", "running", "thinking", "idle", "waiting_approval", "paused", "done", "failed", "stopped"] as const;
export type Status = (typeof STATUSES)[number];

export const ROLES = ["", "architect", "delegate"] as const;
export type Role = (typeof ROLES)[number];

export interface BoardSession {
  id: string;
  ticketId: string;
  agentId: string;
  mode: string;
  model: string;
  task: string;
  status: Status;
  role: Role;
  parentSessionId: string;
  broughtUnder: boolean;
  branch: string;
  pendingApprovals: number;
  openInterviewCount: number;
  interruptedAt: Date | null;
  createdAt: Date;
  updatedAt: Date;
}

/** The process behind the session is gone for good. */
export const isTerminal = (s: BoardSession): boolean => s.status === "done" || s.status === "failed" || s.status === "stopped";

/** Sessions in the order they were started. */
export function inStartOrder(sessions: readonly BoardSession[]): BoardSession[] {
  return [...sessions].sort((a, b) => a.createdAt.getTime() - b.createdAt.getTime() || (a.id < b.id ? -1 : 1));
}

/** Adds or replaces `session` by id. */
export function upsertSession(list: readonly BoardSession[], session: BoardSession): BoardSession[] {
  const i = list.findIndex((s) => s.id === session.id);
  if (i < 0) return [...list, session];
  const next = [...list];
  next[i] = session;
  return next;
}
