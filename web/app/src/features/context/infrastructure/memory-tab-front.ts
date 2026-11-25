// TabFront in memory, for tests.

import type { TabFront } from "../application/ports";

export interface MemoryTabFront extends TabFront {
  set(front: boolean): void;
}

export function memoryTabFront(initial = false): MemoryTabFront {
  let front = initial;
  const listeners = new Set<(front: boolean) => void>();
  return {
    subscribe(listener) {
      listeners.add(listener);
      listener(front);
      return () => void listeners.delete(listener);
    },
    set(next) {
      front = next;
      for (const l of [...listeners]) l(next);
    },
  };
}
