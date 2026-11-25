// A Feed in memory, for tests and for development without a server.

import type { Feed, FeedHandlers, FeedStatus } from "../kernel/feed";

export interface MemoryFeed extends Feed {
  /** The stream sends `data` on `path`. */
  emit(path: string, data: unknown): void;
  /** The stream's state for `path` changes. */
  setStatus(path: string, status: FeedStatus): void;
  /** How many followers `path` has. */
  followers(path: string): number;
}

export function memoryFeed(): MemoryFeed {
  const followers = new Map<string, Set<FeedHandlers>>();
  const statuses = new Map<string, FeedStatus>();
  return {
    follow(path, handlers) {
      if (!followers.has(path)) followers.set(path, new Set());
      followers.get(path)!.add(handlers);
      handlers.onStatus?.(statuses.get(path) ?? "connecting");
      return () => void followers.get(path)?.delete(handlers);
    },
    emit(path, data) {
      for (const f of [...(followers.get(path) ?? [])]) f.onMessage(data);
    },
    setStatus(path, status) {
      statuses.set(path, status);
      for (const f of [...(followers.get(path) ?? [])]) f.onStatus?.(status);
    },
    followers: (path) => followers.get(path)?.size ?? 0,
  };
}
