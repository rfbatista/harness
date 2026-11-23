import type { BoardSession } from "../domain/session";

export const T0 = Date.UTC(2026, 10, 23, 12, 0, 0);
let sequence = 0;

/** A running session; each call starts one minute after the one before. */
export function session(fields: Partial<BoardSession> = {}): BoardSession {
  sequence += 1;
  const at = new Date(T0 + sequence * 60_000);
  return {
    id: `s${sequence}`,
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
