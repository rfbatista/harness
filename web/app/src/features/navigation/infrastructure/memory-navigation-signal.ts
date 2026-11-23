import type { NavigationSignal } from "../application/ports";

export interface MemoryNavigationSignal extends NavigationSignal {
  /** The person moves to `path`. */
  emit(path: string): void;
}

export function memoryNavigationSignal(): MemoryNavigationSignal {
  const listeners = new Set<(path: string) => void>();
  return {
    emit: (path) => {
      for (const listener of [...listeners]) listener(path);
    },
    subscribe(listener) {
      listeners.add(listener);
      return () => void listeners.delete(listener);
    },
  };
}
