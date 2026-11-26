// The rail's small ports in memory, for tests and for development without a server.

import type { Announcer, FeaturePrefs, PageOpener, RailPrefs, Scheduler } from "../application/ports";

export function memoryRailPrefs(initial: readonly string[] = []): RailPrefs & { collapsed(): string[] } {
  let keys = [...initial];
  return { collapsed: () => keys, setCollapsed: (next) => void (keys = [...next]) };
}

export function memoryFeaturePrefs(open: readonly string[] = []): FeaturePrefs {
  const opened = new Set(open);
  return {
    isOpen: (id) => opened.has(id),
    setOpen(id, value) {
      if (value) opened.add(id);
      else opened.delete(id);
    },
  };
}

export function memoryAnnouncer(): Announcer & { said: string[] } {
  const said: string[] = [];
  return { said, announce: (text) => void said.push(text) };
}

export function memoryPageOpener(): PageOpener & { opened: string[] } {
  const opened: string[] = [];
  return { opened, open: (url) => void opened.push(url) };
}

/** A scheduler that only moves when told: `advance(ms)` runs what is due. */
export function manualScheduler(): Scheduler & { advance(ms: number): void; pending(): number[] } {
  let now = 0;
  let next = 1;
  const timers = new Map<number, { at: number; run: () => void }>();
  return {
    after(ms, run) {
      const id = next++;
      timers.set(id, { at: now + ms, run });
      return () => void timers.delete(id);
    },
    advance(ms) {
      now += ms;
      for (const [id, t] of [...timers].sort((a, b) => a[1].at - b[1].at)) {
        if (t.at <= now && timers.delete(id)) t.run();
      }
    },
    pending: () => [...timers.values()].map((t) => t.at - now),
  };
}
