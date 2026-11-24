import type { SessionContext } from "../domain/session-context";
import type { ContextSource } from "./ports";

/** Reads the context of one session. */
export function loadContext(source: ContextSource, sessionId: string, signal?: AbortSignal): Promise<SessionContext> {
  return source.load(sessionId, signal);
}
