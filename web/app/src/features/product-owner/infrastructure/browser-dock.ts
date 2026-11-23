// The dock's ports in the browser: its memory in local storage, the screen's width, and page opening.

import type { DockMemory, PageNavigator, RememberedDock, Screen } from "../application/ports";
import { DockMemoryDto } from "./dto/dock-memory-dto";

const KEY = "harness.productOwner";
/** The docked panel needs room for a third column: 64 rem, as the shell's other layouts. */
const WIDE = "(min-width: 64rem)";

export interface StorageLike {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
}

/** The browser's local storage, or none where it is refused (a private window): the panel then forgets. */
export function browserStorage(win: Window): StorageLike {
  try {
    return win.localStorage;
  } catch {
    return { getItem: () => null, setItem: () => {} };
  }
}

export function browserDockMemory(storage: StorageLike): DockMemory {
  const read = (): DockMemoryDto => {
    try {
      return DockMemoryDto.fromJSON(JSON.parse(storage.getItem(KEY) ?? "null"));
    } catch {
      return DockMemoryDto.fromJSON(null);
    }
  };
  return {
    load: (projectId) => {
      const held = read();
      return { open: held.open, width: held.width ?? 27, threadId: held.threads[projectId] ?? null };
    },
    save(projectId, state: RememberedDock) {
      const held = read();
      const threads = { ...held.threads };
      if (state.threadId === null) delete threads[projectId];
      else threads[projectId] = state.threadId;
      try {
        storage.setItem(KEY, JSON.stringify(new DockMemoryDto(state.open, state.width, threads)));
      } catch {
        // Storage is full or refused: the panel works, it only forgets.
      }
    },
  };
}

export function browserScreen(win: Window): Screen {
  const wide = win.matchMedia(WIDE);
  return {
    narrow: () => !wide.matches,
    onChange(listener) {
      wide.addEventListener("change", listener);
      return () => wide.removeEventListener("change", listener);
    },
  };
}

export function browserPageNavigator(location: Location): PageNavigator {
  return { open: (href) => location.assign(href) };
}
