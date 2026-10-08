// The option anatomy the Listbox select, the Combobox and the Picker dialog
// share: reading options from a native <select> (data-description, data-meta,
// data-tone, data-pinned, <optgroup>), noticing when its value changes, and
// drawing option rows (status dot, label, description, meta, check, match).

/**
 * @typedef {{ value: string, label: string, description?: string, meta?: string,
 *   tone?: string, group?: string, disabled?: boolean, pinned?: boolean }} Option
 */

/** data-tone on an option → the .status dot's data-state. */
const TONE_STATE = { signal: "running", attention: "waiting", danger: "failed" };

/**
 * The select's options in document order, with their option data.
 * @param {HTMLSelectElement} select
 * @returns {Option[]}
 */
export function readOptions(select) {
  return [...select.options].map((o) => {
    const group = o.parentElement instanceof HTMLOptGroupElement ? o.parentElement : null;
    /** @type {Option} */
    const option = { value: o.value, label: o.label || o.textContent.trim() };
    if (o.dataset.description) option.description = o.dataset.description;
    if (o.dataset.meta) option.meta = o.dataset.meta;
    if (o.dataset.tone) option.tone = o.dataset.tone;
    if (group?.label) option.group = group.label;
    if (o.disabled || group?.disabled) option.disabled = true;
    if (o.hasAttribute("data-pinned")) option.pinned = true;
    return option;
  });
}

/** The values selected in the select, in option order. */
export function selectedValues(select) {
  return [...select.options].filter((o) => o.selected).map((o) => o.value);
}

/**
 * Calls `onChange` (once per task) whenever script sets the select's value:
 * `select.value`, `selectedIndex`, or an option's `selected` (which is how
 * Alpine's x-model writes a select). The browser fires no event for those,
 * so the accessors are wrapped on this select and its options only.
 * @param {HTMLSelectElement} select
 * @param {() => void} onChange
 * @returns {{ track: () => void }}  call track() after options are added
 */
export function watchValue(select, onChange) {
  let queued = false;
  const notify = () => {
    if (queued) return;
    queued = true;
    queueMicrotask(() => {
      queued = false;
      onChange();
    });
  };
  const wrap = (target, proto, prop) => {
    if (Object.prototype.hasOwnProperty.call(target, prop)) return;
    const d = Object.getOwnPropertyDescriptor(proto, prop);
    Object.defineProperty(target, prop, {
      configurable: true,
      get() {
        return d.get.call(this);
      },
      set(v) {
        d.set.call(this, v);
        notify();
      },
    });
  };
  wrap(select, HTMLSelectElement.prototype, "value");
  wrap(select, HTMLSelectElement.prototype, "selectedIndex");
  const track = () => {
    for (const o of select.options) wrap(o, HTMLOptionElement.prototype, "selected");
  };
  track();
  return { track };
}

/**
 * Sets the select's value as a person would: then a bubbling `change` (and
 * `input`), so x-model, x-on:change and forms see it.
 * @param {HTMLSelectElement} select
 * @param {string[]} values  one value, or several for a multiple select
 */
export function commitSelect(select, values) {
  const want = new Set(values);
  for (const o of select.options) o.selected = want.has(o.value);
  if (!select.multiple && !values.length) select.selectedIndex = -1;
  select.dispatchEvent(new Event("input", { bubbles: true }));
  select.dispatchEvent(new Event("change", { bubbles: true }));
}

/**
 * Appends `text` to `parent`, with the first case-insensitive match of
 * `query` in a <mark>.
 */
export function appendMarked(doc, parent, text, query) {
  const at = query ? text.toLowerCase().indexOf(query.toLowerCase()) : -1;
  if (at < 0) {
    parent.append(text);
    return;
  }
  const mark = doc.createElement("mark");
  mark.textContent = text.slice(at, at + query.length);
  parent.append(text.slice(0, at), mark, text.slice(at + query.length));
}

/**
 * One option row: [dot] label / description, meta, and the check (CSS).
 * @param {Document} doc
 * @param {Option} option
 * @param {{ id: string, selected: boolean, query?: string }} state
 */
export function optionRow(doc, option, { id, selected, query = "" }) {
  const row = doc.createElement("div");
  row.setAttribute("role", "option");
  row.id = id;
  row.dataset.value = option.value;
  row.setAttribute("aria-selected", String(selected));
  if (option.disabled) row.setAttribute("aria-disabled", "true");
  if (option.tone && TONE_STATE[option.tone]) {
    const dot = doc.createElement("span");
    dot.className = "status";
    dot.dataset.dotOnly = "";
    dot.dataset.state = TONE_STATE[option.tone];
    dot.setAttribute("aria-hidden", "true");
    row.append(dot);
  }
  const text = doc.createElement("span");
  text.className = "text";
  const label = doc.createElement("span");
  label.className = "label";
  appendMarked(doc, label, option.label, query);
  text.append(label);
  if (option.description) {
    const description = doc.createElement("span");
    description.className = "description";
    description.id = `${id}-description`;
    description.textContent = option.description;
    text.append(description);
    row.setAttribute("aria-describedby", description.id);
  }
  row.append(text);
  if (option.meta) {
    const meta = doc.createElement("span");
    meta.className = "meta";
    meta.textContent = option.meta;
    row.append(meta);
  }
  return row;
}

/**
 * Draws options into a listbox, under group headings where they have one.
 * @param {HTMLElement} listbox
 * @param {Option[]} options
 * @param {{ idPrefix: string, selected: Set<string>, query?: string }} state
 * @returns {HTMLElement[]} the option rows, in order
 */
export function renderOptions(listbox, options, { idPrefix, selected, query = "" }) {
  const doc = listbox.ownerDocument;
  const rows = [];
  const parts = [];
  let group = null;
  let groupName;
  options.forEach((option, i) => {
    const row = optionRow(doc, option, { id: `${idPrefix}-${i}`, selected: selected.has(option.value), query });
    rows.push(row);
    if (!option.group) {
      group = null;
      groupName = undefined;
      parts.push(row);
      return;
    }
    if (option.group !== groupName) {
      groupName = option.group;
      group = doc.createElement("div");
      group.setAttribute("role", "group");
      const heading = doc.createElement("div");
      heading.className = "group";
      heading.id = `${idPrefix}-g${i}`;
      heading.textContent = option.group;
      group.setAttribute("aria-labelledby", heading.id);
      group.append(heading);
      parts.push(group);
    }
    group.append(row);
  });
  listbox.replaceChildren(...parts);
  return rows;
}

/**
 * The next usable index from `from` by `delta`, clamped (or wrapped) to the
 * list; disabled entries are skipped. -1 when nothing is usable.
 * @param {{ disabled?: boolean }[]} items
 */
export function step(items, from, delta, { wrap = false } = {}) {
  const n = items.length;
  if (!n) return -1;
  const dir = delta < 0 ? -1 : 1;
  let i = from < 0 ? (dir > 0 ? -1 : n) : from;
  let target = i + delta;
  if (wrap) target = ((target % n) + n) % n;
  else target = Math.max(0, Math.min(n - 1, target));
  // Walk from the target towards the direction of travel, then back.
  for (let k = target; k >= 0 && k < n; k += dir) if (!items[k].disabled) return k;
  for (let k = target - dir; k >= 0 && k < n; k -= dir) if (!items[k].disabled) return k;
  return -1;
}

/** The first or last usable index. */
export function edge(items, last = false) {
  return last ? step(items, items.length, -1) : step(items, -1, 1);
}

/** Skeleton rows for a loading list (the existing .skeleton block). */
export function skeletonRows(doc, count = 3) {
  return Array.from({ length: count }, () => {
    const row = doc.createElement("div");
    row.className = "skeleton";
    row.setAttribute("aria-hidden", "true");
    return row;
  });
}

/**
 * A message row: empty, no results, or an error with Retry.
 * @param {Document} doc
 * @param {{ text: string, danger?: boolean, onRetry?: () => void }} message
 */
export function messageRow(doc, { text, danger = false, onRetry }) {
  const row = doc.createElement("div");
  row.className = "message";
  if (danger) row.dataset.tone = "danger";
  const words = doc.createElement("span");
  words.textContent = text;
  row.append(words);
  if (onRetry) {
    const retry = doc.createElement("button");
    retry.type = "button";
    retry.className = "button";
    retry.dataset.size = "sm";
    retry.textContent = "Retry";
    retry.addEventListener("click", onRetry);
    row.append(retry);
  }
  return row;
}
