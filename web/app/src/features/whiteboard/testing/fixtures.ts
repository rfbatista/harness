import type { ReviewRequest, TaskMessage } from "../domain/message";
import type { BoardSession } from "../domain/session";

export const T0 = Date.UTC(2026, 10, 23, 12, 0, 0);
let sequence = 0;

/** A running session; each call starts one minute after the one before. */
export function session(fields: Partial<BoardSession> = {}): BoardSession {
  sequence += 1;
  const at = new Date(T0 + sequence * 60_000);
  return {
    id: `s${sequence}`,
    ticketId: "t1",
    agentId: "a1",
    mode: "",
    model: "",
    task: "",
    status: "running",
    role: "",
    parentSessionId: "",
    broughtUnder: false,
    branch: "",
    pendingApprovals: 0,
    openInterviewCount: 0,
    interruptedAt: null,
    createdAt: at,
    updatedAt: at,
    ...fields,
  };
}

export const AGENT_NAMES = { a1: "frontend-developer", arch: "software-architect", rev: "code-reviewer" } as const;

let messages = 0;

/** A message between two sessions; each call is one minute after the one before. */
export function message(from: string, to: string, fields: Partial<TaskMessage> = {}): TaskMessage {
  messages += 1;
  return { id: `m${messages}`, taskId: "t1", fromSessionId: from, toSessionId: to, kind: "status_report", status: "working", body: "", inReplyTo: "", createdAt: new Date(T0 + 1_800_000 + messages * 60_000), ...fields };
}

let reviews = 0;

export function review(architectSessionId: string, fields: Partial<ReviewRequest> = {}): ReviewRequest {
  reviews += 1;
  return { id: `r${reviews}`, taskId: "t1", architectSessionId, aboutSessionId: "", state: "pending", createdAt: new Date(T0 + 1_800_000 + reviews * 60_000), ...fields };
}
