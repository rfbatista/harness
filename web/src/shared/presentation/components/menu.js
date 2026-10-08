// The Menu button (contract variant 4): a button that opens commands. Items
// run; the menu holds no value (the page re-renders aria-checked on radio and
// checkbox items). Keys: WAI-ARIA APG menu button, real focus on the items.
//
//   <div class="[ menu ]" x-data="menu" x-on:menu-select="runAction" data-align="end">
//     <button type="button" class="[ button ]" data-variant="ghost" aria-label="More actions for Add SSE feed">⋯</button>
//     <div class="[ popup ]" role="menu">
//       <button type="button" role="menuitem" data-action="copy-link">Copy link <kbd>C</kbd></button>
//       <a role="menuitem" href="/history">Open history</a>
//       <hr role="separator">
//       <button type="button" role="menuitem" data-action="delete" data-tone="danger">Delete…</button>
//     </div>
//   </div>
//
// Output: `menu-select` on the wrapper with { action, value? } (value from
// data-value on menuitemradio / menuitemcheckbox). An item with
// aria-disabled="true" stays focusable, so it can be found, and does nothing.

import { defaultEnv, floating, holdDialogCancel, isPrintable, typeahead, uid } from "./popup.js";

export class MenuButton {
  /**
   * @param {HTMLElement} root  the .menu wrapper
   * @param {import("./popup.js").Env} env
   */
  constructor(root, env = defaultEnv()) {
    const trigger = root.querySelector(":scope > button");
    const popup = root.querySelector(':scope > [role="menu"]');
    if (!trigger || !popup) throw new Error('menu: a <button> and a [role="menu"] inside');
    this.root = root;
    this.trigger = trigger;
    this.popup = popup;
    this.env = env;
    this.doc = root.ownerDocument;
    this.typeahead = typeahead(env.now);

    trigger.id ||= uid("menu-trigger");
    popup.id ||= uid("menu");
    popup.classList.add("popup");
    trigger.setAttribute("aria-haspopup", "menu");
    trigger.setAttribute("aria-expanded", "false");
    trigger.setAttribute("aria-controls", popup.id);
    popup.setAttribute("aria-labelledby", trigger.id);
    this.floating = floating(popup, {
      anchor: trigger,
      env,
      onDismiss: () => this.close(),
      align: () => (root.dataset.align === "end" ? "end" : "start"),
    });

    trigger.addEventListener("keydown", (e) => this.triggerKey(e));
    trigger.addEventListener("click", () => (this.isOpen ? this.close({ refocus: true }) : this.open("first")));
    popup.addEventListener("keydown", (e) => this.menuKey(e));
    popup.addEventListener("click", (e) => {
      const item = this.itemOf(e.target);
      if (item) this.activate(item, e);
    });
    popup.addEventListener("pointermove", (e) => {
      const item = this.itemOf(e.target);
      if (item && this.doc.activeElement !== item) this.focusItem(item);
    });
  }

  get isOpen() {
    return this.floating.isOpen;
  }

  /** @returns {HTMLElement[]} */
  items() {
    return [...this.popup.querySelectorAll('[role="menuitem"], [role="menuitemradio"], [role="menuitemcheckbox"]')];
  }

  itemOf(target) {
    const item = target instanceof Element ? target.closest('[role^="menuitem"]') : null;
    return item && this.popup.contains(item) ? /** @type {HTMLElement} */ (item) : null;
  }

  /** @param {"first" | "last"} at */
  open(at) {
    if (this.trigger.disabled) return;
    const sheet = this.floating.isPhone();
    this.popup.querySelector(":scope > header")?.remove();
    if (sheet) {
      // An action sheet titled with the object: the trigger's name.
      const header = this.doc.createElement("header");
      header.setAttribute("aria-hidden", "true");
      header.textContent = this.trigger.getAttribute("aria-label") || this.trigger.textContent.trim();
      this.popup.prepend(header);
    }
    for (const item of this.items()) item.tabIndex = -1;
    this.root.toggleAttribute("data-open", true);
    this.trigger.setAttribute("aria-expanded", "true");
    this.releaseCancel = holdDialogCancel(this.root);
    this.floating.open({ sheet });
    const items = this.items();
    const item = at === "last" ? items[items.length - 1] : items[0];
    if (item) this.focusItem(item);
  }

  /** @param {{ refocus?: boolean }} [options] */
  close({ refocus = false } = {}) {
    if (!this.isOpen) return;
    this.floating.close();
    this.releaseCancel?.();
    this.root.removeAttribute("data-open");
    this.trigger.setAttribute("aria-expanded", "false");
    if (refocus) this.trigger.focus();
  }

  focusItem(item) {
    for (const other of this.items()) other.tabIndex = other === item ? 0 : -1;
    item.focus();
  }

  /** Runs an item: tells the page, closes, and gives the focus back unless the action took it. */
  activate(item, event) {
    if (item.getAttribute("aria-disabled") === "true") {
      event?.preventDefault();
      return;
    }
    const detail = { action: item.dataset.action ?? "" };
    if (item.dataset.value !== undefined) detail.value = item.dataset.value;
    this.close();
    this.root.dispatchEvent(new CustomEvent("menu-select", { bubbles: true, detail }));
    const focused = this.doc.activeElement;
    if (!focused || focused === this.doc.body || this.popup.contains(focused)) this.trigger.focus();
  }

  /** @param {KeyboardEvent} e */
  triggerKey(e) {
    if (this.isOpen) return;
    let at = null;
    if (e.key === "Enter" || e.key === " " || e.key === "ArrowDown") at = "first";
    else if (e.key === "ArrowUp") at = "last";
    if (!at) return;
    e.preventDefault();
    e.stopPropagation();
    this.open(at);
  }

  /** @param {KeyboardEvent} e */
  menuKey(e) {
    const items = this.items();
    const i = items.indexOf(/** @type {HTMLElement} */ (this.doc.activeElement));
    const go = (k) => items.length && this.focusItem(items[(k + items.length) % items.length]);
    switch (e.key) {
      case "ArrowDown":
        go(i + 1);
        break;
      case "ArrowUp":
        go(i < 0 ? items.length - 1 : i - 1);
        break;
      case "Home":
        go(0);
        break;
      case "End":
        go(items.length - 1);
        break;
      case "Escape":
        this.close({ refocus: true });
        break;
      case "Tab":
        this.close();
        return; // the focus moves on
      case "Enter":
      case " ":
        if (i < 0) break;
        // A link follows itself on Enter; everything else runs here.
        if (e.key === "Enter" && items[i] instanceof HTMLAnchorElement && items[i].getAttribute("aria-disabled") !== "true") {
          this.close();
          return;
        }
        this.activate(items[i]);
        break;
      default: {
        if (!isPrintable(e)) return;
        const k = this.typeahead.find(e.key, items.map((it) => it.textContent.trim()), i);
        if (k >= 0) this.focusItem(items[k]);
      }
    }
    e.preventDefault();
    e.stopPropagation();
  }
}

/** Alpine.data("menu", menu()). */
export const menu = (env) => () => ({
  init() {
    this._menu = new MenuButton(this.$el, env ?? defaultEnv());
  },
  destroy() {
    this._menu?.close();
  },
});
