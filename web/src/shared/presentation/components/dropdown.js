// The Listbox select (contract variant 2): a single-value picker with rich
// options, built on the native <select> it wraps. The select stays the source
// of truth: choosing sets its value and fires `change`, so x-model, x-on:change
// and form submit keep working. Without JavaScript the select shows as is.
//
//   <div class="[ dropdown ]" x-data="dropdown" data-clearable data-placeholder="Choose an agent…">
//     <select id="new-session-agent" x-model="agentId">
//       <option value="ux" data-description="Variants, wireframes">ux-designer</option>
//       <option value="dev" data-meta="running" data-tone="signal">developer</option>
//     </select>
//   </div>
//
// Wrapper flags: data-clearable, data-placeholder, data-empty-text, data-size,
// data-fit, data-variant, aria-busy (loading), data-error (+ dropdown-retry).
// Outputs: `change` on the select; `dropdown-change` on the wrapper with
// { value, option: { value, label } }. Keys: WAI-ARIA APG select-only combobox.

import { announce, defaultEnv, floating, holdDialogCancel, isPrintable, sheetHeader, typeahead, uid } from "./popup.js";
import { commitSelect, edge, messageRow, readOptions, renderOptions, skeletonRows, step, watchValue } from "./options.js";

const PAGE = 10;

export class ListboxSelect {
  /**
   * @param {HTMLElement} root  the .dropdown wrapper
   * @param {import("./popup.js").Env} env
   */
  constructor(root, env = defaultEnv()) {
    const select = root.querySelector("select");
    if (!select) throw new Error("dropdown: wrap a <select>");
    this.root = root;
    this.select = select;
    this.env = env;
    this.doc = root.ownerDocument;
    /** @type {import("./options.js").Option[]} */
    this.options = [];
    /** @type {HTMLElement[]} */
    this.rows = [];
    this.active = -1;
    /** The value and label the trigger shows; kept when the option vanishes. */
    this.shown = { value: "", label: "" };
    this.typeahead = typeahead(env.now);
    this.build();
    this.readSelect();
    this.sync();
  }

  // ── Building ────────────────────────────────────────────────────────────
  build() {
    const { doc, select, root } = this;
    const id = select.id || uid("dropdown");
    select.id = id;
    select.hidden = true;
    select.tabIndex = -1;

    const label = (select.labels && select.labels[0]) || null;
    this.labelText = label?.textContent.trim() || select.getAttribute("aria-label") || "";
    let labelId;
    if (label) {
      labelId = label.id || `${id}-label`;
      label.id = labelId;
    } else {
      const hiddenLabel = doc.createElement("span");
      hiddenLabel.hidden = true;
      hiddenLabel.id = labelId = `${id}-label`;
      hiddenLabel.textContent = this.labelText;
      root.append(hiddenLabel);
    }

    const trigger = doc.createElement("div");
    trigger.className = "trigger";
    trigger.id = `${id}-trigger`;
    trigger.setAttribute("role", "combobox");
    trigger.setAttribute("aria-haspopup", "listbox");
    trigger.setAttribute("aria-expanded", "false");
    trigger.setAttribute("aria-controls", `${id}-listbox`);
    trigger.setAttribute("aria-labelledby", `${labelId} ${trigger.id}`);
    if (label) label.htmlFor = trigger.id;
    this.label = label;
    this.trigger = trigger;

    const popup = doc.createElement("div");
    popup.className = "popup";
    const listbox = doc.createElement("div");
    listbox.id = `${id}-listbox`;
    listbox.setAttribute("role", "listbox");
    listbox.tabIndex = -1;
    listbox.setAttribute("aria-labelledby", labelId);
    popup.append(listbox);
    this.popup = popup;
    this.listbox = listbox;
    root.append(trigger, popup);

    this.floating = floating(popup, { anchor: trigger, onDismiss: () => this.close(), env: this.env });

    this.onKey = (e) => this.keydown(e);
    trigger.addEventListener("keydown", this.onKey);
    popup.addEventListener("keydown", this.onKey);
    trigger.addEventListener("click", (e) => {
      if (e.target instanceof Element && e.target.closest(".clear")) return;
      if (this.disabled) return;
      if (this.isOpen) this.close();
      else this.open();
    });
    // Keep the focus on the trigger while the pointer works the list.
    popup.addEventListener("mousedown", (e) => {
      if (!(e.target instanceof Element && e.target.closest("button"))) e.preventDefault();
    });
    listbox.addEventListener("pointermove", (e) => {
      const row = e.target instanceof Element ? e.target.closest('[role="option"]') : null;
      const i = row ? this.rows.indexOf(/** @type {HTMLElement} */ (row)) : -1;
      if (i >= 0 && i !== this.active && !this.options[i].disabled) this.setActive(i, false);
    });
    listbox.addEventListener("click", (e) => {
      const row = e.target instanceof Element ? e.target.closest('[role="option"]') : null;
      const i = row ? this.rows.indexOf(/** @type {HTMLElement} */ (row)) : -1;
      if (i >= 0 && !this.options[i].disabled) this.choose(i, { refocus: true });
    });

    select.addEventListener("change", () => this.sync());
    this.watch = watchValue(select, () => this.sync());
    this.observer = new MutationObserver(() => {
      this.watch.track();
      this.readSelect();
      this.sync({ live: true });
    });
    this.observer.observe(select, { childList: true, subtree: true, characterData: true, attributes: true });
    this.observer.observe(root, { attributes: true, attributeFilter: ["aria-busy", "data-error", "data-empty-text", "data-placeholder", "data-clearable"] });
  }

  destroy() {
    this.close();
    this.observer.disconnect();
    this.trigger.remove();
    this.popup.remove();
    this.select.hidden = false;
    this.select.removeAttribute("tabindex");
    if (this.label) this.label.htmlFor = this.select.id;
  }

  // ── State from the select and the wrapper ───────────────────────────────
  get disabled() {
    return this.select.disabled;
  }
  get isOpen() {
    return this.floating.isOpen;
  }
  get busy() {
    return this.root.getAttribute("aria-busy") === "true";
  }

  readSelect() {
    const activeValue = this.options[this.active]?.value;
    const oldIndex = this.active;
    this.options = readOptions(this.select);
    if (this.isOpen) {
      this.renderList();
      // Live data: keep the active option by value; if it went, the next one.
      const kept = this.options.findIndex((o) => o.value === activeValue);
      const next = kept >= 0 ? kept : step(this.options, Math.min(oldIndex, this.options.length) - 1, 1);
      this.setActive(next, kept >= 0);
    }
  }

  /**
   * The trigger and ARIA follow the select. Never changes the value. After a
   * live update (`live`) that removed the shown option, the trigger keeps its
   * last label: the feature decides what happens to the value.
   */
  sync({ live = false } = {}) {
    const { select, trigger, root } = this;
    const option = select.selectedIndex >= 0 ? select.options[select.selectedIndex] : null;
    const vanished = live && this.shown.value !== "" && !this.options.some((o) => o.value === this.shown.value);
    if (!vanished) {
      this.shown = option ? { value: option.value, label: option.label || option.textContent.trim() } : { value: "", label: "" };
    }
    if (this.disabled) {
      trigger.setAttribute("aria-disabled", "true");
      trigger.tabIndex = -1;
      if (this.isOpen) this.close();
    } else {
      trigger.removeAttribute("aria-disabled");
      trigger.tabIndex = 0;
    }
    for (const attr of ["aria-invalid", "aria-describedby"]) {
      if (select.hasAttribute(attr)) trigger.setAttribute(attr, select.getAttribute(attr));
      else trigger.removeAttribute(attr);
    }
    if (select.required) trigger.setAttribute("aria-required", "true");
    else trigger.removeAttribute("aria-required");

    const placeholder = root.dataset.placeholder;
    const showPlaceholder = !!placeholder && this.shown.value === "";
    const parts = [];
    const text = this.doc.createElement("span");
    text.className = showPlaceholder ? "placeholder" : "value";
    text.textContent = showPlaceholder ? placeholder : this.shown.label;
    parts.push(text);
    if (root.hasAttribute("data-clearable") && this.shown.value !== "" && !this.disabled) {
      const clear = this.doc.createElement("button");
      clear.type = "button";
      clear.className = "clear";
      clear.tabIndex = -1;
      clear.setAttribute("aria-label", `Clear ${this.labelText}`.trim());
      clear.textContent = "×";
      clear.addEventListener("click", () => this.clear());
      parts.push(clear);
    }
    const chevron = this.doc.createElement("span");
    chevron.className = "chevron";
    chevron.setAttribute("aria-hidden", "true");
    parts.push(chevron);
    trigger.replaceChildren(...parts);
    if (this.isOpen) this.renderList();
  }

  // ── The list ────────────────────────────────────────────────────────────
  renderList() {
    const { listbox, popup, doc, root } = this;
    popup.querySelector(":scope > .message")?.remove();
    const error = root.dataset.error;
    if (this.busy) {
      listbox.setAttribute("aria-busy", "true");
      listbox.replaceChildren(...skeletonRows(doc));
      this.rows = [];
      return;
    }
    listbox.removeAttribute("aria-busy");
    this.rows = renderOptions(listbox, this.options, {
      idPrefix: `${this.select.id}-option`,
      selected: new Set([this.shown.value]),
    });
    if (error) {
      popup.append(messageRow(doc, { text: error, danger: true, onRetry: () => this.retry() }));
    } else if (!this.options.length) {
      popup.append(messageRow(doc, { text: root.dataset.emptyText || "Nothing to choose from." }));
    }
    this.markActive(false);
  }

  setActive(i, scroll = true) {
    this.active = i;
    this.markActive(scroll);
  }

  markActive(scroll) {
    this.rows.forEach((row, i) => row.toggleAttribute("data-active", i === this.active));
    const row = this.rows[this.active];
    const owner = this.floating.isOpen && this.popup.hasAttribute("data-sheet") ? this.listbox : this.trigger;
    if (row) {
      owner.setAttribute("aria-activedescendant", row.id);
      if (scroll) row.scrollIntoView?.({ block: "nearest" });
    } else {
      owner.removeAttribute("aria-activedescendant");
    }
  }

  // ── Opening and choosing ────────────────────────────────────────────────
  /** @param {"selected" | "first" | "last"} [at] */
  open(at = "selected") {
    if (this.disabled || this.isOpen) return;
    const sheet = this.floating.isPhone();
    this.popup.querySelector(":scope > header")?.remove();
    if (sheet) {
      this.popup.prepend(sheetHeader(this.doc, { title: this.labelText, button: "Close", onClose: () => this.close({ refocus: true }) }));
    }
    this.root.toggleAttribute("data-open", true);
    this.trigger.setAttribute("aria-expanded", "true");
    this.releaseCancel = holdDialogCancel(this.root);
    this.floating.open({ sheet });
    this.renderList();
    const selected = this.options.findIndex((o) => o.value === this.shown.value && !o.disabled);
    if (at === "first") this.active = edge(this.options);
    else if (at === "last") this.active = edge(this.options, true);
    else this.active = selected >= 0 ? selected : edge(this.options);
    this.markActive(true);
    this.floating.place();
    if (sheet) this.listbox.focus();
    if (this.busy) announce(this.doc, `Loading ${this.labelText.toLowerCase()}…`.replace(/^Loading …$/, "Loading…"));
  }

  /** @param {{ refocus?: boolean }} [options] */
  close({ refocus = false } = {}) {
    if (!this.isOpen) return;
    const hadFocus = this.popup.contains(this.doc.activeElement);
    this.floating.close();
    this.releaseCancel?.();
    this.root.removeAttribute("data-open");
    this.trigger.setAttribute("aria-expanded", "false");
    this.trigger.removeAttribute("aria-activedescendant");
    this.listbox.removeAttribute("aria-activedescendant");
    if (refocus || hadFocus) this.trigger.focus();
  }

  /** @param {number} i @param {{ refocus?: boolean }} [options] */
  choose(i, { refocus = false } = {}) {
    const option = this.options[i];
    if (!option || option.disabled) return;
    this.close({ refocus });
    if (option.value === this.select.value && this.select.selectedIndex === i) return;
    this.shown = { value: option.value, label: option.label };
    commitSelect(this.select, [option.value]);
    this.root.dispatchEvent(
      new CustomEvent("dropdown-change", { bubbles: true, detail: { value: option.value, option: { value: option.value, label: option.label } } }),
    );
  }

  clear() {
    if (this.disabled || (this.select.value === "" && this.shown.value === "")) return;
    this.shown = { value: "", label: "" };
    const empty = this.options.findIndex((o) => o.value === "");
    if (empty >= 0) this.select.selectedIndex = empty;
    commitSelect(this.select, empty >= 0 ? [""] : []);
    this.sync();
    this.root.dispatchEvent(new CustomEvent("dropdown-change", { bubbles: true, detail: { value: "", option: null } }));
    this.trigger.focus();
  }

  retry() {
    this.root.dispatchEvent(new CustomEvent("dropdown-retry", { bubbles: true }));
  }

  // ── Keys ────────────────────────────────────────────────────────────────
  /** @param {KeyboardEvent} e */
  keydown(e) {
    if (this.disabled) return;
    const handled = this.isOpen ? this.keyOpen(e) : this.keyClosed(e);
    if (handled) {
      e.preventDefault();
      e.stopPropagation();
    }
  }

  /** @param {KeyboardEvent} e @returns {boolean} handled */
  keyClosed(e) {
    switch (e.key) {
      case "ArrowDown":
      case "Enter":
      case " ":
        this.open();
        return true;
      case "ArrowUp":
        this.open();
        return true;
      case "Home":
        this.open("first");
        return true;
      case "End":
        this.open("last");
        return true;
      case "Delete":
      case "Backspace":
        if (!this.root.hasAttribute("data-clearable")) return false;
        this.clear();
        return true;
      default:
        if (!isPrintable(e)) return false;
        this.open();
        this.typeTo(e.key);
        return true;
    }
  }

  /** @param {KeyboardEvent} e @returns {boolean} handled */
  keyOpen(e) {
    const o = this.options;
    switch (e.key) {
      case "ArrowDown":
        this.setActive(step(o, this.active, 1));
        return true;
      case "ArrowUp":
        if (e.altKey) {
          this.choose(this.active, { refocus: true });
          return true;
        }
        this.setActive(step(o, this.active, -1));
        return true;
      case "Home":
        this.setActive(edge(o));
        return true;
      case "End":
        this.setActive(edge(o, true));
        return true;
      case "PageDown":
        this.setActive(step(o, this.active, PAGE));
        return true;
      case "PageUp":
        this.setActive(step(o, this.active, -PAGE));
        return true;
      case "Enter":
        if (this.root.dataset.error && !o.length) this.retry();
        else this.choose(this.active, { refocus: true });
        return true;
      case " ":
        if (this.typeahead.typing) {
          this.typeTo(" ");
          return true;
        }
        this.choose(this.active, { refocus: true });
        return true;
      case "Escape":
        this.close({ refocus: true });
        return true;
      case "Tab":
        this.choose(this.active);
        this.close();
        return false; // the focus moves on
      default:
        if (!isPrintable(e)) return true; // an open list keeps the keys
        this.typeTo(e.key);
        return true;
    }
  }

  typeTo(key) {
    const i = this.typeahead.find(
      key,
      this.options.map((o) => o.label),
      this.active,
      (k) => !this.options[k].disabled,
    );
    if (i >= 0) this.setActive(i);
  }
}

/** Alpine.data("dropdown", dropdown()). */
export const dropdown = (env) => () => ({
  init() {
    this._listbox = new ListboxSelect(this.$el, env ?? defaultEnv());
  },
  destroy() {
    this._listbox?.destroy();
  },
});
