// RailSource and RailFeed in memory, for tests and for development without a server.

import type { RailSession } from "../domain/activity";
import type { Task } from "../domain/task";
import { RailSourceError, type RailChange, type RailFeed, type RailFeedStatus, type RailSource, type ServicesCount } from "../application/ports";

export interface MemoryRailSource extends RailSource {
  setSessions(sessions: readonly RailSession[]): void;
  setTasks(tasks: readonly Task[]): void;
  setArchived(tasks: readonly Task[]): void;
  /** The next reads of these lists fail, as a down server would. */
  failing(...lists: ("sessions" | "tasks" | "archived")[]): void;
  /** How many times each list was read. */
  reads: { sessions: number; tasks: number; archived: number };
}

export function memoryRailSource(initial: { sessions?: readonly RailSession[]; tasks?: readonly Task[]; archived?: readonly Task[] } = {}): MemoryRailSource {
  let sessions = [...(initial.sessions ?? [])];
  let tasks = [...(initial.tasks ?? [])];
  let archived = [...(initial.archived ?? [])];
  let down = new Set<string>();
  const reads = { sessions: 0, tasks: 0, archived: 0 };
  const read = async <T>(list: keyof typeof reads, value: () => T[]): Promise<T[]> => {
    reads[list]++;
    await Promise.resolve();
    if (down.has(list)) throw new RailSourceError("NETWORK_ERROR", `${list} is down`);
    return value();
  };
  return {
    sessions: () => read("sessions", () => [...sessions]),
    tasks: () => read("tasks", () => [...tasks]),
    archived: () => read("archived", () => [...archived]),
    setSessions: (s) => void (sessions = [...s]),
    setTasks: (t) => void (tasks = [...t]),
    setArchived: (t) => void (archived = [...t]),
    failing: (...lists) => void (down = new Set(lists)),
    reads,
  };
}

export interface MemoryRailFeed extends RailFeed {
  emit(change: RailChange): void;
  setStatus(status: RailFeedStatus): void;
  followers(): number;
}

export function memoryRailFeed(): MemoryRailFeed {
  type Handlers = { onChange(change: RailChange): void; onStatus?(status: RailFeedStatus): void };
  const followers = new Set<Handlers>();
  let status: RailFeedStatus = "connecting";
  return {
    follow(_projectId, handlers) {
      followers.add(handlers);
      handlers.onStatus?.(status);
      return () => void followers.delete(handlers);
    },
    emit(change) {
      for (const f of [...followers]) f.onChange(change);
    },
    setStatus(next) {
      status = next;
      for (const f of [...followers]) f.onStatus?.(next);
    },
    followers: () => followers.size,
  };
}

export interface MemoryServicesCount extends ServicesCount {
  /** The services feed announces a count. */
  emit(count: { seq: number; running: number }): void;
  setStatus(status: RailFeedStatus): void;
  /** What a read answers next, or null for a read that fails. */
  answer(list: { seq: number; running: number } | null): void;
  followers(): number;
}

export function memoryServicesCount(initial: { seq: number; running: number } = { seq: 0, running: 0 }): MemoryServicesCount {
  type Handlers = { onCount(count: { seq: number; running: number }): void; onStatus?(status: RailFeedStatus): void };
  const followers = new Set<Handlers>();
  let list: { seq: number; running: number } | null = initial;
  return {
    follow(_projectId, handlers) {
      followers.add(handlers);
      return () => void followers.delete(handlers);
    },
    async read() {
      await Promise.resolve();
      if (!list) throw new RailSourceError("NETWORK_ERROR", "list_services is down");
      return list;
    },
    emit: (count) => void [...followers].forEach((f) => f.onCount(count)),
    setStatus: (status) => void [...followers].forEach((f) => f.onStatus?.(status)),
    answer: (next) => void (list = next),
    followers: () => followers.size,
  };
}
