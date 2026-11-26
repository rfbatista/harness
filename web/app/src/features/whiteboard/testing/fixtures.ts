import type { ReviewRequest, TaskMessage } from "../domain/message";
import type { BoardMemory, BoardMemoryKill } from "../domain/memory";
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
    memory: null,
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

const GB = 1024 ** 3;

/** A memory sample of a session, `gb` in use of a 4 GB budget, taken at `at`. */
export function memorySample(sessionId: string, gb: number, fields: Partial<BoardMemory> = {}, at = T0 + 3_600_000): BoardMemory {
  return { sessionId, taskId: "t1", sampledAt: new Date(at), rssBytes: gb * GB, budgetBytes: 4 * GB, processes: 7, heaviest: { pid: 81234, command: "node jest", rssBytes: 0.9 * GB }, ...fields };
}

/** An encerramento por memória of a session, at `at`. */
export function memoryKill(sessionId: string, at: number, fields: Partial<BoardMemoryKill> = {}): BoardMemoryKill {
  return { sessionId, taskId: "t1", killedAt: new Date(at), pid: 81234, command: "node jest", rssBytes: 3.5 * GB, treeRssBytes: 4.4 * GB, budgetBytes: 4 * GB, signal: "SIGTERM", ...fields };
}
