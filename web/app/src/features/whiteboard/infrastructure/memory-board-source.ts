// A BoardSource in memory, for tests and for development without a server.

import type { BoardSource } from "../application/ports";
import type { ReviewRequest, TaskMessage } from "../domain/message";
import type { BoardSession } from "../domain/session";

export interface MemoryBoardSource extends BoardSource {
  /** Replaces what the task's sessions read as. */
  setSessions(sessions: BoardSession[]): void;
  setMessages(messages: TaskMessage[]): void;
  setReviews(reviews: ReviewRequest[]): void;
}

export function memoryBoardSource(initial: BoardSession[] = []): MemoryBoardSource {
  let sessions = initial;
  let messages: TaskMessage[] = [];
  let reviews: ReviewRequest[] = [];
  return {
    sessions: async () => sessions,
    messages: async () => messages,
    pendingReviews: async () => reviews.filter((r) => r.state === "pending"),
    setSessions(next) {
      sessions = next;
    },
    setMessages(next) {
      messages = next;
    },
    setReviews(next) {
      reviews = next;
    },
  };
}
