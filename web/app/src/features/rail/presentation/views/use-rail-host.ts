// What the rail does to the document and to focus, which only a browser can: the
// drawer's attribute on the rail's own element, "/", Esc and the focus trap, the
// arrow keys, focus kept across live regroupings, the open task scrolled into
// view and the tooltip of a truncated title. The presenter holds the state; this
// moves focus and reads the layout.

import { useEffect, useLayoutEffect, useRef } from "react";
import type { RailPresenter, RailViewModel } from "../presenters/rail-presenter";

/** What the rail's arrow keys move between, in the Tasks nav. */
const NAV_ITEMS = "h3 > button, a[data-task-id], .more > details > summary";
const FOCUSABLE = "a[href], button:not([disabled]), input, summary";
/** Where "/" does not type into the filter. */
const NOT_FOR_SLASH = "input, textarea, select, [contenteditable=''], [contenteditable='true'], .xterm, dialog[open], [role=listbox], [role=menu]";

/** Shown and focusable: not hidden, folded away or inside a closed <details>. */
const isShown = (el: Element): boolean => (typeof el.checkVisibility === "function" ? el.checkVisibility() : el.getClientRects().length > 0);

const isNarrow = (win: Window): boolean => win.matchMedia?.("(width < 48rem)").matches ?? false;

/**
 * Scrolls the rail's list just enough to show a row. Not scrollIntoView: in
 * Chrome that also moves where Tab starts from, and the skip link must stay
 * the first Tab stop.
 */
export function reveal(row: Element | null | undefined): void {
  const list = row?.closest(".groups");
  if (!row || !list) return;
  const r = row.getBoundingClientRect();
  const box = list.getBoundingClientRect();
  if (r.bottom > box.bottom) list.scrollTop += r.bottom - box.bottom;
  else if (r.top < box.top) list.scrollTop -= box.top - r.top;
}

const navItems = (host: HTMLElement): HTMLElement[] => {
  const groups = host.querySelector(".groups");
  return groups ? [...groups.querySelectorAll<HTMLElement>(NAV_ITEMS)].filter(isShown) : [];
};

/** The selectors, in order, that a focused row's focus may fall back to once a live change removes it. */
function fallbacks(host: HTMLElement, row: HTMLElement): string[] {
  const items = navItems(host);
  const at = items.indexOf(row);
  const where = (el: HTMLElement | undefined): string => {
    if (!el) return "";
    if (el.dataset["taskId"]) return `.groups a[data-task-id="${el.dataset["taskId"]}"]`;
    const key = el.closest("section")?.dataset["key"];
    return el.matches("summary") ? `.groups section[data-key="${key}"] .more summary` : `.groups section[data-key="${key}"] h3 > button`;
  };
  return [where(row), where(items[at + 1]), where(items[at - 1]), where(row.closest("section")?.querySelector<HTMLElement>("h3 > button") ?? undefined)].filter(Boolean);
}

/**
 * `host` is the rail's own element: the island's mount, which the server draws
 * and the stylesheet addresses as `.rail`. Null while rendering on the server.
 */
export function useRailHost(host: HTMLElement | null, presenter: RailPresenter, vm: RailViewModel): void {
  const { drawerOpen, hasTasks } = vm;
  const wasOpen = useRef(false);
  const focusFilterAfterRender = useRef(false);
  const rescue = useRef<string[]>([]);
  const focused = useRef("");
  const latest = useRef({ hasTasks, drawerOpen });
  latest.current = { hasTasks, drawerOpen };

  // The drawer is the host's data-open; the first paint is closed.
  useLayoutEffect(() => {
    host?.toggleAttribute("data-open", drawerOpen);
  }, [host, drawerOpen]);

  // The drawer opens on the Tasks button's rail-open and closes on rail-close.
  useEffect(() => {
    const win = host?.ownerDocument.defaultView;
    if (!win) return;
    const open = () => presenter.openDrawer();
    const close = () => presenter.closeDrawer();
    win.addEventListener("rail-open", open);
    win.addEventListener("rail-close", close);
    return () => {
      win.removeEventListener("rail-open", open);
      win.removeEventListener("rail-close", close);
    };
  }, [host, presenter]);

  // Where focus goes as the drawer opens, and where it returns as it closes.
  useEffect(() => {
    if (!host) return;
    if (drawerOpen && !wasOpen.current) {
      const target = host.querySelector<HTMLElement>('.groups a[aria-current="page"]') ?? host.querySelector<HTMLElement>(".filter input");
      reveal(target);
      target?.focus();
    } else if (!drawerOpen && wasOpen.current) {
      host.ownerDocument.querySelector<HTMLElement>(".tasks-toggle")?.focus();
    }
    wasOpen.current = drawerOpen;
    if (drawerOpen && focusFilterAfterRender.current) host.querySelector<HTMLElement>(".filter input")?.focus();
    focusFilterAfterRender.current = false;
  }, [host, drawerOpen]);

  // The open task comes into view on the first render in the browser.
  useEffect(() => {
    if (host) reveal(host.querySelector('.groups a[aria-current="page"]'));
  }, [host]);

  // Keys, focus and the tooltip.
  useEffect(() => {
    if (!host) return;
    const doc = host.ownerDocument;
    const win = doc.defaultView;
    if (!win) return;

    const drawerKey = (event: KeyboardEvent) => {
      if (!latest.current.drawerOpen || event.defaultPrevented) return;
      if (event.key === "Escape") {
        event.preventDefault();
        presenter.closeDrawer();
        return;
      }
      if (event.key !== "Tab") return;
      const focusable = [...host.querySelectorAll<HTMLElement>(FOCUSABLE)].filter(isShown);
      const first = focusable[0];
      const last = focusable.at(-1);
      if (!first || !last) return;
      if (event.shiftKey && doc.activeElement === first) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && doc.activeElement === last) {
        event.preventDefault();
        first.focus();
      }
    };

    /** Arrow keys, Home and End in the Tasks nav (an enhancement: Tab still works). */
    const navKey = (event: KeyboardEvent) => {
      if (event.metaKey || event.ctrlKey || event.altKey || event.defaultPrevented) return;
      const target = event.target;
      if (!(target instanceof HTMLElement) || !target.closest(".groups")) return;
      const items = navItems(host);
      const at = items.indexOf(target);
      if (at < 0) return;
      const heading = target.matches("h3 > button") ? target : null;
      const go = (el: HTMLElement | undefined | null) => {
        if (!el) return;
        event.preventDefault();
        el.focus();
      };
      switch (event.key) {
        case "ArrowDown":
          return go(items[at + 1]);
        case "ArrowUp":
          return go(items[at - 1]);
        case "Home":
          return go(items[0]);
        case "End":
          return go(items.at(-1));
        case "ArrowLeft":
          if (!heading) return go(target.closest("section")?.querySelector<HTMLElement>("h3 > button"));
          if (heading.getAttribute("aria-expanded") === "true") {
            event.preventDefault();
            heading.click();
          }
          return;
        case "ArrowRight":
          if (!heading) return;
          if (heading.getAttribute("aria-expanded") === "false") {
            event.preventDefault();
            heading.click();
          } else go(items[at + 1]?.closest("section") === heading.closest("section") ? items[at + 1] : null);
          return;
      }
    };

    /** "/" anywhere but a text field, the terminal or an open dialog or popup: the filter. */
    const globalKey = (event: KeyboardEvent) => {
      if (event.key !== "/" || event.metaKey || event.ctrlKey || event.altKey || event.defaultPrevented) return;
      const target = event.target;
      if (target instanceof Element && target.closest(NOT_FOR_SLASH)) return;
      if (doc.querySelector("dialog[open]")) return;
      if (!latest.current.hasTasks) return;
      event.preventDefault();
      if (isNarrow(win) && !latest.current.drawerOpen) {
        focusFilterAfterRender.current = true;
        presenter.openDrawer();
      } else host.querySelector<HTMLElement>(".filter input")?.focus();
    };

    const focusIn = (event: FocusEvent) => {
      const row = event.target instanceof Element ? event.target.closest<HTMLElement>("a[data-task-id]") : null;
      focused.current = row?.dataset["taskId"] ?? "";
      presenter.setFocused(focused.current);
      if (row) rescue.current = fallbacks(host, row);
    };
    const focusOut = (event: FocusEvent) => {
      const next = event.relatedTarget instanceof Element ? event.relatedTarget.closest<HTMLElement>("a[data-task-id]") : null;
      if (next?.dataset["taskId"] !== focused.current) {
        focused.current = "";
        presenter.clearFocusedRow();
      }
      // Focus that the person moved away stays away; one that a live change took with its row is brought back.
      if (event.target instanceof Node && event.target.isConnected) rescue.current = [];
    };

    /** A truncated title shows whole as a native tooltip; the open task wraps instead. */
    const tip = (event: MouseEvent) => {
      const a = event.target instanceof Element ? event.target.closest<HTMLAnchorElement>("a[data-task-id]") : null;
      const label = a?.querySelector<HTMLElement>(".label");
      if (!a || !label) return;
      if (label.scrollWidth > label.clientWidth && a.getAttribute("aria-current") !== "page") a.title = label.textContent ?? "";
      else a.removeAttribute("title");
    };

    host.addEventListener("keydown", drawerKey);
    host.addEventListener("keydown", navKey);
    win.addEventListener("keydown", globalKey);
    host.addEventListener("focusin", focusIn);
    host.addEventListener("focusout", focusOut);
    host.addEventListener("mouseover", tip);
    return () => {
      host.removeEventListener("keydown", drawerKey);
      host.removeEventListener("keydown", navKey);
      win.removeEventListener("keydown", globalKey);
      host.removeEventListener("focusin", focusIn);
      host.removeEventListener("focusout", focusOut);
      host.removeEventListener("mouseover", tip);
    };
  }, [host, presenter]);

  // A live change that removed the focused row (it moved to another group) must not drop focus on the page:
  // it goes to the same task where it is now, else the next item, the previous one, the task's heading.
  useLayoutEffect(() => {
    if (!host) return;
    const doc = host.ownerDocument;
    if (host.contains(doc.activeElement) && doc.activeElement !== doc.body) {
      const row = doc.activeElement?.closest<HTMLElement>("a[data-task-id]");
      if (row) rescue.current = fallbacks(host, row);
      return;
    }
    if (doc.activeElement !== doc.body || rescue.current.length === 0) return;
    for (const selector of rescue.current) {
      const el = [...host.querySelectorAll<HTMLElement>(selector)].find(isShown);
      if (el) {
        el.focus();
        break;
      }
    }
    rescue.current = [];
  });
}
