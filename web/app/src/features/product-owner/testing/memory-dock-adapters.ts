// The dock's browser ports in memory, for tests and for the server render.

import type { DockMemory, PageNavigator, RememberedDock, Screen } from "../application/ports";

export interface MemoryDockMemory extends DockMemory {
  readonly saved: readonly { projectId: string; state: RememberedDock }[];
  /** What the browser held before the page loaded. */
  preset(projectId: string, state: RememberedDock): void;
}

export function memoryDockMemory(initial: RememberedDock = { open: false, width: 27, threadId: null }): MemoryDockMemory {
  const held = new Map<string, RememberedDock>();
  let global = initial;
  const saved: { projectId: string; state: RememberedDock }[] = [];
  return {
    saved,
    preset: (projectId, state) => void ((global = { ...global, open: state.open, width: state.width }), held.set(projectId, state)),
    load: (projectId) => ({ ...global, threadId: held.get(projectId)?.threadId ?? null }),
    save(projectId, state) {
      global = { ...global, open: state.open, width: state.width };
      held.set(projectId, state);
      saved.push({ projectId, state });
    },
  };
}

export interface MemoryScreen extends Screen {
  setNarrow(narrow: boolean): void;
  readonly listeners: number;
}

export function memoryScreen(narrow = false): MemoryScreen {
  const listeners = new Set<() => void>();
  return {
    narrow: () => narrow,
    onChange(listener) {
      listeners.add(listener);
      return () => void listeners.delete(listener);
    },
    setNarrow(next) {
      narrow = next;
      for (const l of [...listeners]) l();
    },
    get listeners() {
      return listeners.size;
    },
  };
}

export function memoryNavigator(): PageNavigator & { readonly opened: readonly string[] } {
  const opened: string[] = [];
  return { opened, open: (href) => void opened.push(href) };
}
