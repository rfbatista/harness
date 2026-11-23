// A BoardSource in memory, for tests and for development without a server.

import type { BoardSource } from "../application/ports";
import type { BoardSession } from "../domain/session";

export interface MemoryBoardSource extends BoardSource {
  /** Replaces what the task's sessions read as. */
  setSessions(sessions: BoardSession[]): void;
}

export function memoryBoardSource(initial: BoardSession[] = []): MemoryBoardSource {
  let sessions = initial;
  return {
    sessions: async () => sessions,
    setSessions(next) {
      sessions = next;
    },
  };
}
