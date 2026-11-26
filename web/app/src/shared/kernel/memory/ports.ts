// What the memory UI needs from the outside world. They sit in the kernel with the rules because both the session
// island and the whiteboard use them, and the layer rules let neither a domain nor a feature reach into another feature.

import type { BudgetSource, MemoryKill, MemorySample } from "./sample";

export interface MemoryPrevention {
  heapMb: number;
  testWorkers: number;
}

/** GET /api/sessions/:id/memory. */
export interface MemoryRead {
  sample: MemorySample;
  /** The kills of the session's run. */
  kills: MemoryKill[];
  budgetSource: BudgetSource;
  prevention: MemoryPrevention;
}

export interface MemorySource {
  /** The initial read of a live session; null when the session is not live (404). Rejects on any other failure. */
  read(sessionId: string, signal?: AbortSignal): Promise<MemoryRead | null>;
}

export interface MemoryHandlers {
  onSample(sample: MemorySample): void;
  onKill(kill: MemoryKill): void;
  /** The stream dropped and came back: what was missed is gone. */
  onResync(): void;
}

/** A session's own stream. */
export interface MemoryStream {
  follow(sessionId: string, handlers: MemoryHandlers): () => void;
}
