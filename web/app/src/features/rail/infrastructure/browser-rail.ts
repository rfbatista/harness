// The rail's small ports over the browser: the rail_collapsed cookie, the
// browser's preference for each feature row, the stream bar's announcement, and
// the page the person goes to.

import type { Announcer, FeaturePrefs, PageOpener, RailPrefs, Scheduler } from "../application/ports";

const COOKIE = "rail_collapsed";
const YEAR = 365 * 24 * 60 * 60;
const KEY = /^[a-z_]+$/;

/** The rail_collapsed cookie: comma-separated status keys, one preference per browser for every project. The server reads the same cookie, so the first paint is folded. */
export function cookieRailPrefs(doc: Pick<Document, "cookie">): RailPrefs {
  return {
    setCollapsed(keys) {
      const value = [...new Set(keys)].filter((k) => KEY.test(k)).join(",");
      doc.cookie = `${COOKIE}=${value}; Path=/; Max-Age=${value ? YEAR : 0}; SameSite=Lax`;
    },
  };
}

type Storage = Pick<globalThis.Storage, "getItem" | "setItem" | "removeItem">;

const PREFIX = "harness:rail-feature:";

/** Each feature row's open state in this browser's localStorage (ADR 0037): `harness:rail-feature:<id>` is "open". Storage may be missing or throw. */
export function browserFeaturePrefs(win: Pick<Window, "localStorage">): FeaturePrefs {
  const safe = <T>(read: (s: Storage) => T, otherwise: T): T => {
    try {
      const s = win.localStorage;
      return s ? read(s) : otherwise;
    } catch {
      return otherwise;
    }
  };
  return {
    isOpen: (id) => safe((s) => s.getItem(PREFIX + id) === "open", false),
    setOpen: (id, open) => safe((s) => (open ? s.setItem(PREFIX + id, "open") : s.removeItem(PREFIX + id)), undefined),
  };
}

/** The stream bar's polite region listens for the window's `rail-announce`. */
export function windowAnnouncer(win: Pick<Window, "dispatchEvent">): Announcer {
  return { announce: (text) => void win.dispatchEvent(new CustomEvent("rail-announce", { detail: text })) };
}

export function windowPageOpener(location: Pick<Location, "assign">): PageOpener {
  return { open: (url) => location.assign(url) };
}

export function timeoutScheduler(win: Pick<Window, "setTimeout" | "clearTimeout">): Scheduler {
  return {
    after(ms, run) {
      const id = win.setTimeout(run, ms);
      return () => win.clearTimeout(id);
    },
  };
}

/** The Tasks button opens the rail's drawer: the window's `rail-open`, which the rail also hears from anyone else. */
export function windowRailOpener(win: Pick<Window, "dispatchEvent">): () => void {
  return () => void win.dispatchEvent(new CustomEvent("rail-open"));
}
