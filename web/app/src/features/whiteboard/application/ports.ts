// What the whiteboard needs from outside: the task's sessions, and the person's host page.

import type { ReviewRequest, TaskMessage } from "../domain/message";
import type { BoardSession } from "../domain/session";

export type BoardSourceErrorCode = "NETWORK_ERROR" | "BAD_RESPONSE" | "UNKNOWN";

export class BoardSourceError extends Error {
  readonly code: BoardSourceErrorCode;
  constructor(code: BoardSourceErrorCode, message: string) {
    super(message);
    this.name = "BoardSourceError";
    this.code = code;
  }
}

export interface BoardSource {
  /** The sessions of a task. */
  sessions(ticketId: string, signal?: AbortSignal): Promise<BoardSession[]>;
  /** Every message between the sessions of a task, oldest first. */
  messages(ticketId: string, signal?: AbortSignal): Promise<TaskMessage[]>;
  /** The review requests of a task that wait on the person. */
  pendingReviews(ticketId: string, signal?: AbortSignal): Promise<ReviewRequest[]>;
  /** Follows the live feed of a project; returns the function that stops it. */
  follow(projectId: string, follower: BoardFollower): () => void;
}

/** What the project's live feed tells the board. */
export type BoardEvent =
  | { kind: "session"; session: BoardSession }
  | { kind: "session-deleted"; id: string }
  | { kind: "message"; message: TaskMessage }
  | { kind: "review"; review: ReviewRequest };

/** "paused": dropped and retrying; "resynced": back after a drop, so changes may have been missed. */
export type LiveStatus = "connecting" | "live" | "paused" | "resynced";

export interface BoardFollower {
  onEvent(event: BoardEvent): void;
  onStatus(status: LiveStatus): void;
}
