// The Combobox (contract variant 3): type to filter, for long or query-loaded
// lists, several values, or free text. Keys: WAI-ARIA APG editable combobox
// with list autocomplete; focus stays in the input.
//
// Static options: wrap a <select> (multiple for several values); it stays the
// source of truth and is filtered here.
//   <div class="[ dropdown ]" x-data="combobox" data-free-text data-free-text-label="Run “{q}”">
//     <select id="run-choice"><option value="server">server</option></select>
//   </div>
// Query options: a method on the enclosing Alpine scope, named by data-source,
// (query, signal) → Promise<Option[]>, and a hidden input that carries the value.
//   <div class="[ dropdown ]" x-data="combobox" data-source="searchTasks">
//     <label for="task">Task</label><input type="hidden" id="task" name="task">
//   </div>
//
// Option: { value, label, description?, meta?, tone?, group?, disabled? }.
// <option data-pinned> stays at the top whatever the filter.
// Flags: data-multiple, data-free-text (+ data-free-text-label), data-min-chars,
// data-clearable, data-placeholder, data-empty-text, data-size, data-fit,
// data-variant, aria-busy, data-error (+ dropdown-retry) with a wrapped select.
// Outputs: `change` on the select or hidden input; `dropdown-change` on the
// wrapper with { value, values, option, created } (created: free text).

import { announce, defaultEnv, floating, holdDialogCancel, sheetHeader, uid } from "./popup.js";
import { commitSelect, edge, messageRow, readOptions, renderOptions, selectedValues, skeletonRows, step, watchValue } from "./options.js";

const PAGE = 10;
const DEBOUNCE = 150;

/** @typedef {import("./options.js").Option & { created?: boolean }} Entry */

export class Combobox {
  /**
   * @param {HTMLElement} root  the .dropdown wrapper
   * @param {{ env?: import("./popup.js").Env, source?: ((query: string, signal: AbortSignal) => Promise<import("./options.js").Option[]>) | null }} [options]
   */
  constructor(root, { env = defaultEnv(), source = null } = {}) {
    this.root = root;
    this.env = env;
    this.doc = root.ownerDocument;
    this.select = root.querySelector("select");
    this.hidden = this.select ? null : root.querySelector('input[type="hidden"]');
    if (!this.select && !(source && this.hidden)) throw new Error("combobox: wrap a <select>, or give data-source and a hidden input");
    this.source = this.select ? null : source;
    this.multiple = this.select ? this.select.multiple : root.hasAttribute("data-multiple");
    this.freeText = root.hasAttribute("data-free-text");
    this.minChars = Number(root.dataset.minChars || 0);
    /** @type {Entry[]} every option (static), or the latest results (query) */
    this.options = [];
    /** @type {Entry[]} what the list shows */
    this.shown = [];
    /** @type {HTMLElement[]} */
    this.rows = [];
    this.active = -1;
    this.activeChip = -1;
    /** The person typed since the last choice: the text filters. */
    this.dirty = false;
    /** Query mode: the chosen values and their labels. */
    this.values = this.hidden?.value ? [this.hidden.value] : [];
    this.labels = new Map(this.values.map((v) => [v, this.hidden.dataset.label || v]));
    this.loading = false;
    this.error = "";
    this.build();
    if (this.select) this.readSelect();
    this.sync();
  }

  // ── Building ────────────────────────────────────────────────────────────
  build() {
    const { doc, root } = this;
    const control = this.select ?? this.hidden;
    const id = control.id || uid("combobox");
    control.id = id;
    if (this.select) {
      this.select.hidden = true;
      this.select.tabIndex = -1;
    }
    const label = (control.labels && control.labels[0]) || null;
    this.label = label;
    this.labelText = label?.textContent.trim() || control.getAttribute("aria-label") || "";

    const field = doc.createElement("div");
    field.className = "trigger";
    const chips = doc.createElement("span");
    chips.className = "chips";
    chips.setAttribute("role", "list");
    chips.setAttribute("aria-label", `Selected ${this.labelText.toLowerCase()}`.trim());
    const input = doc.createElement("input");
    input.id = `${id}-input`;
    input.type = "text";
    input.autocomplete = "off";
    input.spellcheck = false;
    input.setAttribute("role", "combobox");
    input.setAttribute("aria-autocomplete", "list");
    input.setAttribute("aria-expanded", "false");
    input.setAttribute("aria-controls", `${id}-listbox`);
    if (root.dataset.placeholder) input.placeholder = root.dataset.placeholder;
    if (label) label.htmlFor = input.id;
    else if (this.labelText) input.setAttribute("aria-label", this.labelText);
    const marker = doc.createComment("combobox input");
    const toggle = doc.createElement("button");
    toggle.type = "button";
    toggle.className = "toggle";
    toggle.tabIndex = -1;
    toggle.setAttribute("aria-label", `Show ${this.labelText.toLowerCase()}`.trim());
    const chevron = doc.createElement("span");
    chevron.className = "chevron";
    chevron.setAttribute("aria-hidden", "true");
    toggle.append(chevron);
    const ends = doc.createElement("span");
    ends.className = "ends";
    field.append(chips, marker, input, ends, toggle);
    Object.assign(this, { field, chips, input, marker, toggle, ends });

    const popup = doc.createElement("div");
    popup.className = "popup";
    const listbox = doc.createElement("div");
    listbox.id = `${id}-listbox`;
    listbox.setAttribute("role", "listbox");
    if (label) listbox.setAttribute("aria-labelledby", label.id || (label.id = `${id}-label`));
    if (this.multiple) listbox.setAttribute("aria-multiselectable", "true");
    popup.append(listbox);
    this.popup = popup;
    this.listbox = listbox;
    root.append(field, popup);

    this.floating = floating(popup, { anchor: field, onDismiss: () => this.close(), env: this.env });

    input.addEventListener("keydown", (e) => this.keydown(e));
    input.addEventListener("input", () => this.typed());
    input.addEventListener("focus", () => {
      if (!this.dirty) input.select();
      this.renderChips();
    });
    const onFocusOut = (e) => {
      if (!this.moving) this.maybeBlur(e.relatedTarget);
    };
    root.addEventListener("focusout", onFocusOut);
    popup.addEventListener("focusout", onFocusOut);
    toggle.addEventListener("mousedown", (e) => e.preventDefault());
    toggle.addEventListener("click", () => {
      if (this.isOpen) this.close();
      else this.open({ move: false });
      input.focus();
    });
    field.addEventListener("click", (e) => {
      if (e.target === field) input.focus();
    });
    popup.addEventListener("mousedown", (e) => {
      if (!(e.target instanceof Element && e.target.closest("button, input"))) e.preventDefault();
    });
    listbox.addEventListener("pointermove", (e) => {
      const i = this.rowIndex(e.target);
      if (i >= 0 && i !== this.active && !this.shown[i].disabled) this.setActive(i, false);
    });
    listbox.addEventListener("click", (e) => {
      const i = this.rowIndex(e.target);
      if (i >= 0 && !this.shown[i].disabled) this.choose(i);
    });

    if (this.select) {
      this.select.addEventListener("change", () => this.sync());
      this.watch = watchValue(this.select, () => this.sync());
      this.observer = new MutationObserver(() => {
        this.watch.track();
        this.readSelect();
        this.sync();
      });
      this.observer.observe(this.select, { childList: true, subtree: true, characterData: true, attributes: true });
    } else {
      this.observer = new MutationObserver(() => this.sync());
    }
    this.observer.observe(root, { attributes: true, attributeFilter: ["aria-busy", "data-error", "data-empty-text"] });
  }

  destroy() {
    this.close();
    this.observer.disconnect();
    this.field.remove();
    this.popup.remove();
    if (this.select) {
      this.select.hidden = false;
      this.select.removeAttribute("tabindex");
      if (this.label) this.label.htmlFor = this.select.id;
    }
  }

  rowIndex(target) {
    const row = target instanceof Element ? target.closest('[role="option"]') : null;
    return row ? this.rows.indexOf(/** @type {HTMLElement} */ (row)) : -1;
  }

  // ── State ───────────────────────────────────────────────────────────────
  get isOpen() {
    return this.floating.isOpen;
  }
  get disabled() {
    return !!(this.select ?? this.hidden).disabled;
  }
  get busy() {
    return this.loading || this.root.getAttribute("aria-busy") === "true";
  }
  get query() {
    return this.dirty ? this.input.value.trim() : "";
  }

  /** The chosen values, from the select or (query mode) our own list. */
  currentValues() {
    if (!this.select) return this.values;
    return selectedValues(this.select).filter((v) => v !== "" || this.options.some((o) => o.value === "" && o.label));
  }

  labelOf(value) {
    const option = this.select ? [...this.select.options].find((o) => o.value === value) : null;
    return option ? option.label || option.textContent.trim() : this.labels.get(value) ?? value;
  }

  readSelect() {
    // A free-text value lives in the select as option[data-created]: a value, not a suggestion.
    this.options = readOptions(this.select).filter((_, i) => !this.select.options[i].hasAttribute("data-created"));
    if (this.isOpen) this.filter({ keepActive: true });
  }

  /** The field follows the select (or our values). Never changes the value. */
  sync() {
    const control = this.select ?? this.hidden;
    this.input.disabled = this.disabled;
    this.toggle.disabled = this.disabled;
    if (this.disabled && this.isOpen) this.close();
    for (const attr of ["aria-invalid", "aria-describedby"]) {
      if (control.hasAttribute(attr)) this.input.setAttribute(attr, control.getAttribute(attr));
      else this.input.removeAttribute(attr);
    }
    if (this.select?.required) this.input.setAttribute("aria-required", "true");
    if (!this.multiple && !this.dirty) {
      const [value] = this.currentValues();
      this.input.value = value === undefined ? "" : this.labelOf(value);
    }
    this.renderChips();
    this.renderEnds();
    if (this.isOpen) this.render();
  }

  // ── The field: chips, spinner, clear ────────────────────────────────────
  renderChips() {
    if (!this.multiple) return;
    const { doc } = this;
    const values = this.currentValues();
    const chips = values.map((value, i) => {
      const chip = doc.createElement("span");
      chip.className = "chip";
      chip.setAttribute("role", "listitem");
      if (i === this.activeChip) chip.dataset.active = "";
      const label = this.labelOf(value);
      chip.append(label);
      const remove = doc.createElement("button");
      remove.type = "button";
      remove.tabIndex = -1;
      remove.setAttribute("aria-label", `Remove ${label}`);
      remove.textContent = "×";
      remove.addEventListener("mousedown", (e) => e.preventDefault());
      remove.addEventListener("click", () => this.toggleValue(value));
      chip.append(remove);
      return chip;
    });
    this.chips.replaceChildren(...chips);
    // At rest, at most two rows of chips, then "+N more".
    if (this.root.contains(doc.activeElement) || !chips.length || !this.field.isConnected) return;
    const rows = () => new Set(chips.filter((c) => !c.hidden).map((c) => c.offsetTop)).size;
    let hidden = 0;
    let more = null;
    while (rows() > 2 && hidden < chips.length) {
      chips[chips.length - 1 - hidden].hidden = true;
      hidden++;
      more ??= doc.createElement("span");
      more.className = "chip";
      more.dataset.more = "";
      more.setAttribute("role", "listitem");
      more.textContent = `+${hidden} more`;
      if (!more.isConnected) this.chips.append(more);
    }
  }

  renderEnds() {
    const parts = [];
    if (this.busy) {
      const spinner = this.doc.createElement("span");
      spinner.className = "spinner";
      spinner.setAttribute("aria-hidden", "true");
      parts.push(spinner);
    } else if (this.input.value && !this.disabled && (this.dirty || this.root.hasAttribute("data-clearable"))) {
      const clear = this.doc.createElement("button");
      clear.type = "button";
      clear.className = "clear";
      clear.tabIndex = -1;
      clear.setAttribute("aria-label", `Clear ${this.labelText}`.trim());
      clear.textContent = "×";
      clear.addEventListener("mousedown", (e) => e.preventDefault());
      clear.addEventListener("click", () => this.clearText());
      parts.push(clear);
    }
    this.ends.replaceChildren(...parts);
    this.toggle.hidden = this.busy;
  }

  // ── The list ────────────────────────────────────────────────────────────
  /** Recomputes what the list shows from the text. */
  filter({ keepActive = false } = {}) {
    const q = this.query;
    const activeValue = this.shown[this.active]?.value;
    if (this.source) {
      this.shown = [...this.options];
    } else {
      const needle = q.toLowerCase();
      const pinned = this.options.filter((o) => o.pinned);
      const matches = this.options.filter((o) => !o.pinned && (!needle || o.label.toLowerCase().includes(needle)));
      this.shown = [...pinned, ...matches];
    }
    if (this.freeText && q && !this.options.some((o) => o.label.toLowerCase() === q.toLowerCase())) {
      const template = this.root.dataset.freeTextLabel || "Use “{q}”";
      this.shown.push({ value: q, label: template.replace("{q}", q), created: true });
    }
    this.render();
    if (keepActive) {
      const kept = this.shown.findIndex((o) => o.value === activeValue);
      this.setActive(kept >= 0 ? kept : step(this.shown, Math.min(this.active, this.shown.length) - 1, 1), false);
    } else if (q) {
      // Typing makes the first match active (past the pinned rows).
      const first = this.shown.findIndex((o) => !o.pinned && !o.disabled);
      this.setActive(first >= 0 ? first : edge(this.shown), false);
    }
    if (this.dirty && !this.busy) {
      const n = this.shown.filter((o) => !o.pinned && !o.created).length;
      announce(this.doc, n ? `${n} ${n === 1 ? "option" : "options"}` : "No matches");
    }
  }

  render() {
    const { listbox, popup, doc, root } = this;
    popup.querySelector(":scope > .message")?.remove();
    popup.querySelector(":scope > footer")?.remove();
    if (this.busy) {
      listbox.setAttribute("aria-busy", "true");
      listbox.replaceChildren(...skeletonRows(doc));
      this.rows = [];
      return;
    }
    listbox.removeAttribute("aria-busy");
    const chosen = new Set(this.currentValues());
    this.rows = renderOptions(listbox, this.shown, { idPrefix: `${this.input.id}-option`, selected: chosen, query: this.query });
    this.shown.forEach((o, i) => {
      if (o.created) this.rows[i].setAttribute("aria-selected", "false");
    });
    const q = this.query;
    const error = this.error || root.dataset.error;
    if (error) {
      popup.append(messageRow(doc, { text: error, danger: true, onRetry: () => this.retry() }));
    } else if (!this.options.length && !q) {
      popup.append(messageRow(doc, { text: root.dataset.emptyText || "Nothing to choose from." }));
    } else if (q && !this.shown.some((o) => !o.pinned)) {
      popup.append(messageRow(doc, { text: `Nothing matches “${q}”.` }));
    }
    if (this.multiple && chosen.size) {
      const footer = doc.createElement("footer");
      const count = doc.createElement("span");
      count.textContent = `${chosen.size} selected`;
      const clear = doc.createElement("button");
      clear.type = "button";
      clear.className = "button";
      clear.dataset.variant = "ghost";
      clear.dataset.size = "sm";
      clear.textContent = "Clear all";
      clear.addEventListener("click", () => this.commit([], null));
      footer.append(count, clear);
      popup.append(footer);
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
    if (row) {
      this.input.setAttribute("aria-activedescendant", row.id);
      if (scroll) row.scrollIntoView?.({ block: "nearest" });
    } else {
      this.input.removeAttribute("aria-activedescendant");
    }
  }

  // ── Opening ─────────────────────────────────────────────────────────────
  /** @param {{ move?: boolean, at?: "selected" | "last" }} [options] */
  open({ move = true, at = "selected" } = {}) {
    if (this.disabled) return;
    if (!this.isOpen) {
      const sheet = this.floating.isPhone();
      this.popup.querySelector(":scope > header")?.remove();
      this.popup.querySelector(":scope > .search")?.remove();
      if (sheet) {
        const search = this.doc.createElement("div");
        search.className = "search";
        this.moving = true;
        search.append(this.chips, this.input);
        this.popup.prepend(sheetHeader(this.doc, { title: this.labelText, button: "Done", onClose: () => this.close() }), search);
      }
      this.root.toggleAttribute("data-open", true);
      this.input.setAttribute("aria-expanded", "true");
      this.releaseCancel = holdDialogCancel(this.root);
      this.floating.open({ sheet, tall: true });
      if (sheet) {
        this.input.focus();
        this.moving = false;
      }
      // Typing schedules its own (debounced) query; opening empty asks once.
      if (this.source && !this.dirty && !this.loading && !this.options.length) this.load();
    }
    this.active = -1;
    this.filter();
    if (move && this.active < 0) {
      const [value] = this.currentValues();
      const selected = this.shown.findIndex((o) => o.value === value && !o.disabled);
      this.setActive(selected >= 0 ? selected : at === "last" ? edge(this.shown, true) : edge(this.shown));
    }
    this.floating.place();
  }

  close() {
    if (!this.isOpen) return;
    const search = this.popup.querySelector(":scope > .search");
    const refocus = this.popup.contains(this.doc.activeElement);
    this.floating.close();
    this.releaseCancel?.();
    if (search) {
      this.moving = true;
      this.marker.before(this.chips);
      this.marker.after(this.input);
      this.moving = false;
      search.remove();
      this.popup.querySelector(":scope > header")?.remove();
      if (refocus) this.input.focus();
    }
    this.root.removeAttribute("data-open");
    this.input.setAttribute("aria-expanded", "false");
    this.input.removeAttribute("aria-activedescendant");
    this.active = -1;
  }

  // ── Typing and loading ──────────────────────────────────────────────────
  typed() {
    this.dirty = true;
    this.activeChip = -1;
    this.renderEnds();
    if (this.source) {
      this.schedule();
      if (!this.isOpen && this.input.value.trim().length >= this.minChars) this.open({ move: false });
      return;
    }
    if (!this.isOpen) this.open({ move: false });
    else this.filter();
  }

  schedule() {
    this.env.clearTimeout(this.timer);
    this.controller?.abort();
    this.timer = this.env.setTimeout(() => this.load(), DEBOUNCE);
  }

  /** Query mode: asks the source for the text; only the latest answer renders. */
  async load() {
    const q = this.query;
    if (q.length < this.minChars) {
      this.options = [];
      this.filter();
      return;
    }
    this.controller?.abort();
    const controller = new AbortController();
    this.controller = controller;
    this.loading = true;
    this.error = "";
    this.renderEnds();
    if (this.isOpen) this.render();
    announce(this.doc, "Searching…");
    try {
      const options = await this.source(q, controller.signal);
      if (controller.signal.aborted) return;
      this.options = options;
    } catch (err) {
      if (controller.signal.aborted || err?.name === "AbortError") return;
      this.options = [];
      this.error = "Could not load the options.";
    }
    this.loading = false;
    this.renderEnds();
    if (this.isOpen) this.filter();
  }

  retry() {
    if (this.source) this.load();
    else this.root.dispatchEvent(new CustomEvent("dropdown-retry", { bubbles: true }));
  }

  // ── Choosing ────────────────────────────────────────────────────────────
  choose(i) {
    const entry = this.shown[i];
    if (!entry || entry.disabled) return;
    if (this.multiple) {
      this.toggleValue(entry.value, entry);
      this.input.value = "";
      this.dirty = false;
      this.filter({ keepActive: true });
      return;
    }
    this.close();
    this.commit([entry.value], entry);
  }

  toggleValue(value, entry = null) {
    const values = this.currentValues();
    const on = !values.includes(value);
    const next = on ? [...values, value] : values.filter((v) => v !== value);
    this.commit(next, entry ?? { value, label: this.labelOf(value) });
    announce(this.doc, `${entry?.label ?? this.labelOf(value)} ${on ? "added" : "removed"}`);
    if (this.isOpen) this.render();
  }

  /**
   * Writes the values to the select or hidden input, then tells the page.
   * @param {string[]} values
   * @param {Entry | null} entry
   */
  commit(values, entry) {
    const created = !!entry?.created;
    if (entry && !this.select) this.labels.set(entry.value, created ? entry.value : entry.label);
    if (this.select) {
      this.writeCreated(created ? entry.value : null);
      commitSelect(this.select, values);
    } else {
      this.values = values;
      this.writeHidden(values);
    }
    this.dirty = false;
    this.sync();
    const option = entry ? { value: entry.value, label: created ? entry.value : entry.label } : null;
    this.root.dispatchEvent(
      new CustomEvent("dropdown-change", {
        bubbles: true,
        detail: { value: values[values.length - 1] ?? "", values: [...values], option, created },
      }),
    );
  }

  /** Free text in a wrapped select: one option[data-created] carries it. */
  writeCreated(value) {
    let option = this.select.querySelector("option[data-created]");
    if (value === null) {
      option?.remove();
      return;
    }
    if (!option) {
      option = this.doc.createElement("option");
      option.dataset.created = "";
      this.select.append(option);
      this.watch.track();
    }
    option.value = value;
    option.textContent = value;
  }

  writeHidden(values) {
    const { hidden } = this;
    for (const extra of this.root.querySelectorAll("input[data-combobox-value]")) extra.remove();
    hidden.value = values[0] ?? "";
    for (const value of values.slice(1)) {
      const extra = /** @type {HTMLInputElement} */ (hidden.cloneNode());
      extra.removeAttribute("id");
      extra.dataset.comboboxValue = "";
      extra.value = value;
      hidden.after(extra);
    }
    hidden.dispatchEvent(new Event("input", { bubbles: true }));
    hidden.dispatchEvent(new Event("change", { bubbles: true }));
  }

  clearText() {
    this.input.value = "";
    this.dirty = true;
    this.renderEnds();
    if (this.isOpen) this.filter();
    this.input.focus();
  }

  /**
   * Focus left the combobox (for `to`, or nowhere): close, and settle the text.
   * @param {EventTarget | null} to
   */
  maybeBlur(to) {
    if (to instanceof Node && (this.root.contains(to) || this.popup.contains(to))) return;
    this.close();
    if (!this.dirty || this.multiple) {
      this.renderChips();
      return;
    }
    const text = this.input.value.trim();
    const exact = this.options.find((o) => o.label.toLowerCase() === text.toLowerCase() && !o.disabled);
    if (!text && this.root.hasAttribute("data-clearable")) this.commit([], null);
    else if (exact) this.commit([exact.value], exact);
    else if (this.freeText && text) this.commit([text], { value: text, label: text, created: true });
    else {
      // Never leave half-typed text looking chosen: back to the value's label.
      this.dirty = false;
      this.sync();
    }
  }

  // ── Keys ────────────────────────────────────────────────────────────────
  /** @param {KeyboardEvent} e */
  keydown(e) {
    if (this.multiple && this.chipKey(e)) {
      e.preventDefault();
      e.stopPropagation();
      return;
    }
    const handled = this.isOpen ? this.keyOpen(e) : this.keyClosed(e);
    if (handled) {
      e.preventDefault();
      e.stopPropagation();
    }
  }

  /** @param {KeyboardEvent} e */
  keyClosed(e) {
    switch (e.key) {
      case "ArrowDown":
        this.open({ move: !e.altKey });
        return true;
      case "ArrowUp":
        this.open({ at: "last" });
        return true;
      case "Escape":
        if (!this.input.value) return false;
        this.clearText();
        return true;
      case "Enter":
        // Enter on a closed list submits the form; typed free text goes first.
        if (this.freeText && this.dirty && !this.multiple) this.maybeCommitText();
        return false;
      default:
        return false;
    }
  }

  /** @param {KeyboardEvent} e */
  keyOpen(e) {
    const s = this.shown;
    switch (e.key) {
      case "ArrowDown":
        if (!e.altKey) this.setActive(step(s, this.active, 1));
        return true;
      case "ArrowUp":
        if (e.altKey) {
          if (this.active >= 0) this.choose(this.active);
          this.close();
          return true;
        }
        this.setActive(step(s, this.active, -1));
        return true;
      case "PageDown":
        this.setActive(step(s, this.active, PAGE));
        return true;
      case "PageUp":
        this.setActive(step(s, this.active, -PAGE));
        return true;
      case "Enter":
        if (this.active >= 0) this.choose(this.active);
        else if (this.error || this.root.dataset.error) this.retry();
        else this.close();
        return true;
      case "Escape":
        this.close();
        return true;
      case "Tab":
        if (!this.multiple && this.dirty && this.active >= 0) this.choose(this.active);
        this.close();
        return false;
      default:
        return false; // Home, End and typing belong to the input
    }
  }

  maybeCommitText() {
    const text = this.input.value.trim();
    const exact = this.options.find((o) => o.label.toLowerCase() === text.toLowerCase() && !o.disabled);
    if (exact) this.commit([exact.value], exact);
    else if (text) this.commit([text], { value: text, label: text, created: true });
  }

  /** Backspace into the chips, ←/→ between them, Delete or Backspace removes. */
  chipKey(e) {
    const values = this.currentValues();
    const atStart = this.input.selectionStart === 0 && this.input.selectionEnd === 0;
    if (this.activeChip < 0) {
      if (e.key === "Backspace" && atStart && values.length) {
        this.activeChip = values.length - 1;
        this.renderChips();
        return true;
      }
      if (e.key === "ArrowLeft" && atStart && values.length) {
        this.activeChip = values.length - 1;
        this.renderChips();
        return true;
      }
      return false;
    }
    switch (e.key) {
      case "ArrowLeft":
        this.activeChip = Math.max(0, this.activeChip - 1);
        break;
      case "ArrowRight":
        this.activeChip = this.activeChip + 1 < values.length ? this.activeChip + 1 : -1;
        break;
      case "Backspace":
      case "Delete": {
        const value = values[this.activeChip];
        this.activeChip = values.length > 1 ? Math.min(this.activeChip, values.length - 2) : -1;
        this.toggleValue(value);
        break;
      }
      default:
        this.activeChip = -1;
        this.renderChips();
        return false;
    }
    this.renderChips();
    return true;
  }
}

/** Alpine.data("combobox", combobox()): data-source names a method in scope. */
export const combobox = (env) => () => ({
  init() {
    const name = this.$el.dataset.source;
    const source = name ? (query, signal) => this[name](query, signal) : null;
    this._combobox = new Combobox(this.$el, { env: env ?? defaultEnv(), source });
  },
  destroy() {
    this._combobox?.destroy();
  },
});
