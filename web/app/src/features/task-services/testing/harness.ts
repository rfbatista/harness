import type { Clipboard, Scheduler } from "../application/ports";
import { storagePanelPreferences } from "../infrastructure/storage-panel-preferences";
import { PanelPresenter } from "../presentation/presenters/panel-presenter";
import type { MemoryServicesSource } from "./memory-services-source";
import { TASK } from "./fixtures";

export const settle = () => new Promise((resolve) => setImmediate(resolve));

/** A clock and timers the test runs by hand. */
export function manualScheduler(start = Date.parse("2026-11-25T10:10:00Z")) {
  let now = start;
  const timers: { at: number; fn: () => void; live: boolean }[] = [];
  const scheduler: Scheduler = {
    now: () => now,
    after: (ms, fn) => {
      const timer = { at: now + ms, fn, live: true };
      timers.push(timer);
      return () => void (timer.live = false);
    },
  };
  return {
    scheduler,
    advance(ms: number) {
      now += ms;
      for (const t of timers.filter((t) => t.live && t.at <= now)) {
        t.live = false;
        t.fn();
      }
    },
    pending: () => timers.filter((t) => t.live).length,
  };
}

export function memoryStorage(init: Record<string, string> = {}) {
  const data = new Map(Object.entries(init));
  return { getItem: (k: string) => data.get(k) ?? null, setItem: (k: string, v: string) => void data.set(k, v), data };
}

export async function panel(source: MemoryServicesSource, opts: { storage?: ReturnType<typeof memoryStorage>; clipboard?: Clipboard } = {}) {
  const clock = manualScheduler();
  const copied: string[] = [];
  const storage = opts.storage ?? memoryStorage();
  const presenter = new PanelPresenter({
    source,
    preferences: storagePanelPreferences(storage),
    clipboard: opts.clipboard ?? { copy: async (t) => void copied.push(t) },
    scheduler: clock.scheduler,
    projectId: "p1",
    taskId: TASK,
    taskTitle: "Fix the build",
  });
  await presenter.start();
  return { presenter, clock, copied, storage };
}

export const rows = (p: PanelPresenter) => p.viewModel.groups.flatMap((g) => g.rows);
