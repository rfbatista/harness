// Clipboard in memory, for tests.

import type { Clipboard } from "../application/ports";

export interface MemoryClipboard extends Clipboard {
  /** What was copied, in order. */
  readonly copied: readonly string[];
}

export function memoryClipboard(): MemoryClipboard {
  const copied: string[] = [];
  return { copied, copy: async (text) => void copied.push(text) };
}
