// ContextFeed in memory, for tests and for development without a server.

import type { ContextFeed } from "../application/ports";
import type { ContextChanged } from "../domain/session-context";

export interface MemoryContextFeed extends ContextFeed {
  /** The server announces a change. */
  emit(change: ContextChanged): void;
  /** How many followers a project has. */
  followers(projectId: string): number;
}

export function memoryContextFeed(): MemoryContextFeed {
  const followers = new Map<string, Set<(change: ContextChanged) => void>>();
  return {
    follow(projectId, onChanged) {
      if (!followers.has(projectId)) followers.set(projectId, new Set());
      followers.get(projectId)!.add(onChanged);
      return () => void followers.get(projectId)?.delete(onChanged);
    },
    emit(change) {
      // The memory feed has one project in play: every follower hears it.
      for (const set of followers.values()) for (const f of [...set]) f(change);
    },
    followers: (projectId) => followers.get(projectId)?.size ?? 0,
  };
}
