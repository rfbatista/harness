// The graph of a task: who started whom. The person is the root (ADR 0108).

import { isTerminal, inStartOrder, type BoardSession } from "./session";

export const YOU_ID = "you";

export interface StartedEdge {
  id: string;
  from: string;
  to: string;
}

/** An edge from each session to the one that started it; sessions nobody on the task started hang from You. */
export function startedEdges(sessions: readonly BoardSession[]): StartedEdge[] {
  const ids = new Set(sessions.map((s) => s.id));
  return inStartOrder(sessions).map((s) => {
    const from = s.parentSessionId !== "" && ids.has(s.parentSessionId) ? s.parentSessionId : YOU_ID;
    return { id: `${from}>${s.id}`, from, to: s.id };
  });
}

/** The task's architect, when it has one. */
export const architectOf = (sessions: readonly BoardSession[]): BoardSession | undefined => sessions.find((s) => s.role === "architect");

export const allEnded = (sessions: readonly BoardSession[]): boolean => sessions.length > 0 && sessions.every(isTerminal);
