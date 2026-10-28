// The skill-and-bundle search picker, as members a page component mixes in
// under a namespace (first version: ticket 06 upgrades it to the full ARIA
// combobox without changing this interface).
//
//   Object.defineProperties(page, Object.getOwnPropertyDescriptors(entryPickerMixin("bundlePicker", source)))
//
// The page owns the chosen entries; the picker asks for what it needs through
// `source` and reports what the person did. Members it adds, for ns "x":
//   fields   xQuery, xOpen, xActive
//   getter   xView  { listId, expanded, activeId, options[{key,label,kind,hint,chosen,active,id,mark}],
//                     hidden, hiddenNote, empty, emptyNote, chips[{key,label,kind,missing,mono}], noChips }
//   methods  xFocus, xBlur, xInput, xKey(event), xPick(event), xUnpick(event), xAdd(key), xRemove(key)
// Markup: entryPicker(ns, …) in agents/save.templ.

import { pickerKey } from "../../domain/entryPicker.js";

/**
 * @typedef {object} PickerSource
 * @property {(page: any, query: string) => { options: { key: string, kind: string, label: string, hint: string, chosen: boolean }[], hidden: number, empty: boolean }} options
 * @property {(page: any) => { key: string, label: string, kind: string, missing: boolean }[]} chips  the chosen entries, in order
 * @property {(page: any, key: string) => void} add  adds an entry; the page ignores one it already has
 * @property {(page: any, key: string) => void} remove
 */

/** @param {string} ns @param {PickerSource} source */
export function entryPickerMixin(ns, source) {
  const q = `${ns}Query`;
  const open = `${ns}Open`;
  const active = `${ns}Active`;
  const view = `${ns}View`;
  const optionId = (i) => `${ns}-opt-${i}`;

  return {
    [q]: "",
    [open]: false,
    [active]: 0,

    get [view]() {
      const query = this[q];
      const found = source.options(this, query);
      const at = found.options.length ? Math.min(this[active], found.options.length - 1) : 0;
      const chips = source.chips(this).map((c) => ({ removeLabel: `Remove ${c.label}`, ...c, mono: c.kind === "skill" }));
      return {
        listId: `${ns}-list`,
        expanded: this[open],
        activeId: this[open] && found.options.length ? optionId(at) : null,
        options: found.options.map((o, i) => ({ ...o, id: optionId(i), active: this[open] && i === at, mark: o.chosen ? "✓" : o.kind === "create" ? "+" : "" })),
        hidden: found.hidden,
        hiddenNote: found.hidden ? `${found.hidden} more: keep typing to narrow the list` : "",
        empty: found.empty,
        emptyNote: query.trim() ? `No skill or bundle matches “${query.trim()}”.` : "Nothing to pick from.",
        chips,
        noChips: chips.length === 0,
      };
    },

    [`${ns}Focus`]() {
      this[open] = true;
    },
    [`${ns}Blur`]() {
      this[open] = false;
    },
    /** Typing narrows the list and puts the first match under the cursor. */
    [`${ns}Input`]() {
      this[open] = true;
      this[active] = 0;
    },
    [`${ns}Key`](event) {
      const v = this[view];
      const r = pickerKey({ open: this[open], active: this[active], query: this[q], chosen: v.chips.map((c) => c.key) }, event.key, v.options.map((o) => o.key));
      if (!r.handled) return;
      event.preventDefault();
      this[open] = r.open;
      this[active] = r.active;
      if (r.add !== undefined) {
        source.add(this, r.add);
        this[q] = "";
      }
      if (r.removeLast) source.remove(this, v.chips[v.chips.length - 1].key);
    },
    /** A mouse press on an option; it keeps the focus in the input. */
    [`${ns}Pick`](event) {
      event.preventDefault();
      source.add(this, event.currentTarget.dataset.key);
      this[q] = "";
    },
    [`${ns}Unpick`](event) {
      source.remove(this, event.currentTarget.dataset.key);
    },
    [`${ns}Add`](key) {
      source.add(this, key);
    },
    [`${ns}Remove`](key) {
      source.remove(this, key);
    },
  };
}
