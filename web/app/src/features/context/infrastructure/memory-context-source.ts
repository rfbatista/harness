// ContextSource in memory, for tests and for development without a server.

import { ContextSourceError, type ContextSource } from "../application/ports";
import type { SessionContext } from "../domain/session-context";

export interface MemoryContextSource extends ContextSource {
  /** What the server answers for the context's session. */
  serve(context: SessionContext): void;
  /** The full text the server has for a piece. */
  servePiece(sessionId: string, entryId: string, pieceId: string, text: string): void;
  /** The next calls fail with this error until `heal()`. */
  failWith(error: ContextSourceError): void;
  heal(): void;
  /** Holds loads until the returned function releases them. */
  hold(): () => void;
  /** The session ids loaded, in order. */
  readonly loaded: readonly string[];
  /** The pieces fetched in full, as "session/entry/piece", in order. */
  readonly fetched: readonly string[];
}

export function memoryContextSource(): MemoryContextSource {
  const contexts = new Map<string, SessionContext>();
  const pieces = new Map<string, string>();
  const loaded: string[] = [];
  const fetched: string[] = [];
  let failure: ContextSourceError | null = null;
  let held: Promise<void> | null = null;
  return {
    loaded,
    fetched,
    serve: (context) => void contexts.set(context.sessionId, context),
    servePiece: (session, entry, piece, text) => void pieces.set(`${session}/${entry}/${piece}`, text),
    failWith: (error) => void (failure = error),
    heal: () => void (failure = null),
    hold() {
      let release = () => {};
      held = new Promise<void>((resolve) => {
        release = () => {
          held = null;
          resolve();
        };
      });
      return release;
    },
    async load(sessionId, signal) {
      signal?.throwIfAborted();
      loaded.push(sessionId);
      if (failure) throw failure;
      if (held) await held;
      signal?.throwIfAborted();
      const context = contexts.get(sessionId);
      if (!context) throw new ContextSourceError("SESSION_NOT_FOUND", "no such session");
      return context;
    },
    async piece(sessionId, entryId, pieceId, signal) {
      signal?.throwIfAborted();
      fetched.push(`${sessionId}/${entryId}/${pieceId}`);
      if (failure) throw failure;
      const text = pieces.get(`${sessionId}/${entryId}/${pieceId}`);
      if (text === undefined) throw new ContextSourceError("CONTEXT_PIECE_NOT_FOUND", "no such piece");
      return text;
    },
  };
}
