import type { ScopeAddress } from "../application/scope-address";

/** An address in memory: `history` is every value written, for a test to read. */
export function memoryScopeAddress(initial = ""): ScopeAddress & { history: string[]; back(to: string): void } {
  let current = initial;
  const listeners = new Set<(id: string) => void>();
  return {
    history: [],
    read: () => current,
    write(id) {
      current = id;
      this.history.push(id);
    },
    follow(listener) {
      listeners.add(listener);
      return () => void listeners.delete(listener);
    },
    back(to) {
      current = to;
      for (const l of [...listeners]) l(to);
    },
  };
}
