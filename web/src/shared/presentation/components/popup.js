// What the scripted dropdowns (Listbox select, Combobox, Menu button) share
// about their floating .popup: the top layer, placement beside the trigger,
// the phone sheet, one open at a time, light dismiss, Esc before any enclosing
// dialog, type-ahead and the polite live region. Plain functions, no Alpine.
//
//   const popup = floating(popupEl, { anchor: triggerEl, onDismiss: close, env });
//   popup.open({ sheet: popup.isPhone() });   // shows it, places it, claims the slot
//   popup.close();

/** Below this the scripted variants open as bottom sheets (the system's breakpoint). */
export const PHONE_QUERY = "(width < 48rem)";

const GAP = 4; // px between the trigger and the popup
const EDGE = 8; // px the popup keeps from the viewport's edges
const FLIP_MIN = 240; // px: flip up when less room than this (or its height) is below

/**
 * The environment the components run in; tests replace parts of it.
 * @typedef {{ window: Window, document: Document, now: () => number,
 *   setTimeout: typeof setTimeout, clearTimeout: typeof clearTimeout }} Env
 */

/** @returns {Env} */
export function defaultEnv() {
  return {
    window,
    document,
    now: () => Date.now(),
    setTimeout: (fn, ms) => window.setTimeout(fn, ms),
    clearTimeout: (id) => window.clearTimeout(id),
  };
}

// ── One open at a time ────────────────────────────────────────────────────
let openClose = null;

/** Closes whichever popup is open, then records `close` as the open one. */
function claim(close) {
  if (openClose && openClose !== close) openClose();
  openClose = close;
}

function release(close) {
  if (openClose === close) openClose = null;
}

/**
 * Where a popup of `size` goes beside `anchor` (both DOMRect-like) in a
 * viewport of `view` ({ width, height }): below, or above when the room below
 * is short and there is more above; never outside the viewport.
 * @returns {{ top: number, left: number, minWidth: number, maxHeight: number, placement: "bottom" | "top" }}
 */
export function placement(anchor, size, view, { align = "start" } = {}) {
  const below = view.height - anchor.bottom - GAP - EDGE;
  const above = anchor.top - GAP - EDGE;
  const up = below < Math.min(size.height, FLIP_MIN) && above > below;
  const room = Math.max(0, up ? above : below);
  const height = Math.min(size.height, room);
  const top = up ? anchor.top - GAP - height : anchor.bottom + GAP;
  const width = Math.max(size.width, anchor.width);
  const wanted = align === "end" ? anchor.right - width : anchor.left;
  const left = Math.max(EDGE, Math.min(wanted, view.width - width - EDGE));
  return { top, left, minWidth: anchor.width, maxHeight: room, placement: up ? "top" : "bottom" };
}

/**
 * Wires a .popup element to its anchor.
 * @param {HTMLElement} el  the .popup (popover="manual" is set here)
 * @param {{ anchor: HTMLElement, onDismiss: () => void, env: Env, align?: () => "start" | "end" }} options
 */
export function floating(el, { anchor, onDismiss, env, align = () => "start" }) {
  const { window: win, document: doc } = env;
  el.setAttribute("popover", "manual");
  let isOpen = false;

  const dismiss = () => onDismiss();
  const inside = (node) => node instanceof Node && (el.contains(node) || anchor.contains(node));

  const onPointerDown = (e) => {
    // A tap on a sheet's backdrop targets the popup itself, outside its box.
    if (e.target === el && el.hasAttribute("data-sheet")) {
      const r = el.getBoundingClientRect();
      if (e.clientY < r.top) dismiss();
      return;
    }
    if (!inside(e.target)) dismiss();
  };
  const onScroll = (e) => {
    if (el.hasAttribute("data-sheet") || inside(e.target)) return;
    dismiss();
  };
  const onResize = () => place();

  function place() {
    if (!isOpen || el.hasAttribute("data-sheet")) return;
    el.style.maxBlockSize = "";
    const a = anchor.getBoundingClientRect();
    const p = placement(
      a,
      { width: el.offsetWidth, height: el.offsetHeight },
      { width: win.innerWidth, height: win.innerHeight },
      { align: align() },
    );
    el.style.top = `${p.top}px`;
    el.style.left = `${p.left}px`;
    el.style.setProperty("--_anchor-width", `${p.minWidth}px`);
    el.style.maxBlockSize = `${p.maxHeight}px`;
    el.dataset.placement = p.placement;
  }

  return {
    get isOpen() {
      return isOpen;
    },
    /** True when the viewport is phone-sized: open as a sheet. */
    isPhone() {
      return !!win.matchMedia?.(PHONE_QUERY).matches;
    },
    /** @param {{ sheet?: boolean, tall?: boolean }} [mode] */
    open({ sheet = false, tall = false } = {}) {
      claim(dismiss);
      el.toggleAttribute("data-sheet", sheet);
      el.toggleAttribute("data-tall", sheet && tall);
      el.style.top = el.style.left = el.style.maxBlockSize = "";
      el.style.removeProperty("--_anchor-width");
      if (!isOpen) {
        isOpen = true;
        if (el.isConnected && !el.matches(":popover-open")) el.showPopover?.();
        doc.addEventListener("pointerdown", onPointerDown, true);
        doc.addEventListener("scroll", onScroll, true);
        win.addEventListener("resize", onResize);
      }
      place();
    },
    place,
    close() {
      release(dismiss);
      if (!isOpen) return;
      isOpen = false;
      if (el.isConnected && el.matches(":popover-open")) el.hidePopover?.();
      doc.removeEventListener("pointerdown", onPointerDown, true);
      doc.removeEventListener("scroll", onScroll, true);
      win.removeEventListener("resize", onResize);
      el.removeAttribute("data-placement");
    },
  };
}

/**
 * Keeps Esc for the dropdown while it is open inside a <dialog>: the dialog's
 * cancel (Esc) is swallowed once, so the dropdown closes first.
 * @param {HTMLElement} el  any element inside the dialog
 * @returns {() => void} undo
 */
export function holdDialogCancel(el) {
  const dialog = el.closest("dialog");
  if (!dialog) return () => {};
  const swallow = (e) => e.preventDefault();
  dialog.addEventListener("cancel", swallow);
  return () => dialog.removeEventListener("cancel", swallow);
}

/**
 * Type-ahead over a list of labels: printable keys build a prefix for 500ms;
 * the same letter repeated cycles through the labels starting with it.
 * @param {() => number} now
 */
export function typeahead(now) {
  let buffer = "";
  let last = 0;
  return {
    /** True while a prefix is being typed (Space then belongs to it). */
    get typing() {
      return buffer !== "" && now() - last < 500;
    },
    /**
     * @param {string} key  one printable character
     * @param {string[]} labels
     * @param {number} from  the active index
     * @param {(i: number) => boolean} [usable]  false for disabled entries
     * @returns {number} the matching index, or -1
     */
    find(key, labels, from, usable = () => true) {
      const t = now();
      buffer = t - last < 500 ? buffer + key.toLowerCase() : key.toLowerCase();
      last = t;
      const repeated = [...buffer].every((c) => c === buffer[0]);
      const prefix = repeated ? buffer[0] : buffer;
      const start = repeated ? from + 1 : from;
      for (let k = 0; k < labels.length; k++) {
        const i = (Math.max(0, start) + k) % labels.length;
        if (usable(i) && labels[i].toLowerCase().startsWith(prefix)) return i;
      }
      return -1;
    },
  };
}

/** A single printable character with no command modifier. */
export function isPrintable(e) {
  return e.key.length === 1 && !e.ctrlKey && !e.metaKey && !e.altKey;
}

/**
 * The polite live region the dropdowns announce through (result counts,
 * "Loading…", "design added"). One per document, visually hidden.
 * @param {Document} doc
 */
export function announce(doc, text) {
  let region = doc.getElementById("dropdown-live");
  if (!region) {
    region = doc.createElement("div");
    region.id = "dropdown-live";
    region.className = "visually-hidden";
    region.setAttribute("aria-live", "polite");
    region.setAttribute("role", "status");
    doc.body.append(region);
  }
  region.textContent = text;
}

let ids = 0;
/** A document-unique id with a readable prefix. */
export function uid(prefix) {
  ids += 1;
  return `${prefix}-${ids}`;
}

/**
 * The phone sheet's header: the field's label and a ghost Close / Done.
 * @param {Document} doc
 */
export function sheetHeader(doc, { title, button, onClose }) {
  const header = doc.createElement("header");
  const name = doc.createElement("span");
  name.textContent = title;
  const close = doc.createElement("button");
  close.type = "button";
  close.className = "button";
  close.dataset.variant = "ghost";
  close.dataset.size = "sm";
  close.textContent = button;
  close.addEventListener("click", onClose);
  header.append(name, close);
  return header;
}
