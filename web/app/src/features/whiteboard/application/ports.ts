// What the whiteboard needs from outside: the task's sessions, and the person's host page.

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
}
