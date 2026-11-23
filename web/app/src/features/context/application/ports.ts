// What the context feature needs from the outside world.

import type { SessionContext } from "../domain/session-context";

export type ContextSourceErrorCode = "SESSION_NOT_FOUND" | "CONTEXT_ENTRY_NOT_FOUND" | "CONTEXT_PIECE_NOT_FOUND" | "NETWORK_ERROR" | "BAD_RESPONSE" | "UNKNOWN";

export class ContextSourceError extends Error {
  readonly code: ContextSourceErrorCode;

  constructor(code: ContextSourceErrorCode, message: string) {
    super(message);
    this.name = "ContextSourceError";
    this.code = code;
  }
}

/** Where a session's context comes from (GET /api/sessions/{id}/context). Rejects with a ContextSourceError. */
export interface ContextSource {
  load(sessionId: string, signal?: AbortSignal): Promise<SessionContext>;
  /** The full text of one piece of one entry. */
  piece(sessionId: string, entryId: string, pieceId: string, signal?: AbortSignal): Promise<string>;
}

/** The person's clipboard: Copy on a failed server puts claude's error there. */
export interface Clipboard {
  copy(text: string): Promise<void>;
}
