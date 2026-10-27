// What the four descriptive tabs share: editing one list of the draft through
// a drawer. Spread the result into a tab's component; `this` is then the tab,
// which reaches the page's `draft`, `saved`, `errors`, `errorFor` and
// `isInvalid` through Alpine scope.
//
// The drawer holds a working copy of one row: typing changes the copy, Apply
// writes it into the draft, Cancel throws it away. A row added with Add is in
// the draft at once (so the list shows it) and Cancel drops it again. The tab
// declares the field named by `editKey` as noDrawer(): the drawer's state.

import { addRow, editedRows, pointer, removeRow, rowsAt, setList, toCsv } from "../../domain/descriptive.js";

/** The state of a closed drawer; serial changes with every opening, so the drawer's fields start over. */
export const noDrawer = () => ({ index: -1, work: null, fresh: false, serial: 0 });

const copy = (row) => JSON.parse(JSON.stringify(row));

/**
 * @param {string[]} path the keys from the record to the list @param {() => any} blank a new entry
 * @param {string} editKey the tab's field holding the drawer's state @param {(row: any) => string} keyOf what names a row, to tell edited rows from new ones
 */
export const listEditing = (path, blank, editKey = "editing", keyOf = (row) => row.path ?? row.name ?? row.agent ?? "") => {
  const rowsOf = (self) => rowsAt(self.draft, path);
  const open = (self, index, fresh) => {
    self[editKey] = { index, work: copy(rowsOf(self)[index]), fresh, serial: self[editKey].serial + 1 };
  };
  const close = (self) => {
    self[editKey] = { ...noDrawer(), serial: self[editKey].serial };
  };
  const shown = (self) => {
    const s = self[editKey];
    return s.index >= 0 && s.index < rowsOf(self).length;
  };
  const removeAt = (self, index) => {
    removeRow(self.draft, path, index);
    const s = self[editKey];
    if (s.index === index) close(self);
    else if (s.index > index) s.index -= 1;
  };
  /** Drops the row the drawer was opened on with Add, if that is where it is; returns how many rows before `index` it removed. */
  const dropFresh = (self, index) => {
    const s = self[editKey];
    if (s.index < 0 || !s.fresh) return 0;
    removeRow(self.draft, path, s.index);
    close(self);
    return s.index < index ? 1 : 0;
  };

  return {
    rows() {
      return rowsOf(this);
    },
    /** Adds a row and opens the drawer on it. */
    add() {
      dropFresh(this, -1);
      addRow(this.draft, path, blank());
      open(this, rowsOf(this).length - 1, true);
    },
    /** The Edit button: opens the drawer on the row. */
    edit(index) {
      if (this[editKey].index === index) return;
      open(this, index - dropFresh(this, index), false);
    },
    drawerOpen() {
      return shown(this);
    },
    drawerIndex() {
      return this[editKey].index;
    },
    drawerKey() {
      return String(this[editKey].serial);
    },
    /** The drawer's working copy, as the one row of a list the drawer draws its fields from. */
    drawerRows() {
      return shown(this) ? [this[editKey].work] : [];
    },
    /** Whether the drawer is on the row. */
    isOpen(index) {
      return this[editKey].index === index;
    },
    /** Writes the working copy into the draft and closes the drawer. */
    apply() {
      const s = this[editKey];
      if (s.index < 0) return;
      rowsOf(this).splice(s.index, 1, copy(s.work));
      close(this);
    },
    /** Throws the working copy away; a row added with Add goes with it. */
    cancel() {
      dropFresh(this, -1);
      close(this);
    },
    /** Closes the drawer when the draft was replaced under it. */
    closeDrawer() {
      close(this);
    },
    remove(index) {
      removeAt(this, index);
    },
    /** The drawer's Remove button. */
    removeOpen() {
      removeAt(this, this[editKey].index);
    },
    /** A save the server refused opens the drawer on the first row it found a problem in. */
    openForErrors() {
      const rows = rowsOf(this);
      const at = rows.findIndex((_, i) => this.errors.some((e) => e.path.startsWith(pointer(path, i, ""))));
      if (at >= 0 && this[editKey].index !== at) open(this, at - dropFresh(this, at), false);
    },
    /** Whether the row differs from the saved version (a new row does). */
    isEdited(index) {
      return !!this.saved && editedRows(rowsAt(this.saved.record, path), rowsOf(this), keyOf)[index] === true;
    },
    editLabel(row) {
      return `Edit ${row.path ?? row.name ?? row.agent ?? row.from ?? "entry"}`;
    },
    err(index, field) {
      return this.errorFor(pointer(path, index, field));
    },
    /** The JSON pointer of a field, for the marker an error is found by. */
    ptr(index, field) {
      return pointer(path, index, field);
    },
    removeLabel(row) {
      return `Remove ${row.path ?? row.name ?? row.agent ?? row.from ?? "entry"}`;
    },
    bad(index, field) {
      return this.isInvalid(pointer(path, index, field));
    },
    /** A list field of a row as the text the person edits. */
    csv(row, field) {
      return toCsv(row[field]);
    },
    setCsv(row, field, event) {
      setList(row, field, event.target.value);
    },
  };
};

/** A list's methods under names that carry a suffix, for a tab with two lists: rows becomes rowsRoute. */
export const suffixed = (methods, suffix) => Object.fromEntries(Object.entries(methods).map(([name, fn]) => [`${name}${suffix}`, fn]));
