import type { KeyValueStorage } from "../infrastructure/storage-board-preferences";

/** A storage in memory, for tests and for development without a browser. */
export function memoryStorage(initial: Record<string, string> = {}): KeyValueStorage & { readonly entries: Map<string, string> } {
  const entries = new Map(Object.entries(initial));
  return {
    entries,
    getItem: (key) => entries.get(key) ?? null,
    setItem: (key, value) => void entries.set(key, value),
    removeItem: (key) => void entries.delete(key),
  };
}
