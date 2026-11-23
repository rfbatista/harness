// A BoardSource in memory, for tests and for development without a server.

import type { BoardEvent, BoardFollower, BoardSource, LiveStatus } from "../application/ports";
import type { ReviewRequest, TaskMessage } from "../domain/message";
import type { BoardSession } from "../domain/session";

export interface MemoryBoardSource extends BoardSource {
  /** Replaces what the task's sessions read as. */
  setSessions(sessions: BoardSession[]): void;
  setMessages(messages: TaskMessage[]): void;
  setReviews(reviews: ReviewRequest[]): void;
  /** The feed sends `event`. */
  emit(event: BoardEvent): void;
  /** The feed's state changes. */
  setStatus(status: LiveStatus): void;
  followers(): number;
}

export function memoryBoardSource(initial: BoardSession[] = []): MemoryBoardSource {
  let sessions = initial;
  let messages: TaskMessage[] = [];
  let reviews: ReviewRequest[] = [];
  const followers = new Set<BoardFollower>();
  let status: LiveStatus = "connecting";
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
    follow(_projectId, follower) {
      followers.add(follower);
      follower.onStatus(status);
      return () => void followers.delete(follower);
    },
    emit(event) {
      for (const f of [...followers]) f.onEvent(event);
    },
    setStatus(next) {
      status = next;
      for (const f of [...followers]) f.onStatus(next);
    },
    followers: () => followers.size,
  };
}
