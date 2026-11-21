// NavigationSignal over the window's events: the Alpine client's
// "harness:navigated" (detail.path, after an in-place navigation) and
// "popstate" (the path of the entry landed on).

import type { NavigationSignal } from "../application/ports";

export const NAVIGATED_EVENT = "harness:navigated";

type Target = Pick<EventTarget, "addEventListener" | "removeEventListener"> & { location: Pick<Location, "pathname"> };

export function browserNavigationSignal(win: Target = globalThis as unknown as Target): NavigationSignal {
  return {
    subscribe(listener) {
      const navigated = (event: Event) => {
        const path = (event as CustomEvent<{ path?: unknown }>).detail?.path;
        if (typeof path === "string") listener(path);
      };
      const popped = () => listener(win.location.pathname);
      win.addEventListener(NAVIGATED_EVENT, navigated);
      win.addEventListener("popstate", popped);
      return () => {
        win.removeEventListener(NAVIGATED_EVENT, navigated);
        win.removeEventListener("popstate", popped);
      };
    },
  };
}
