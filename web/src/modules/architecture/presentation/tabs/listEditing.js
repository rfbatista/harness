// What the four descriptive tabs share: editing one list of the draft.
// Spread the result into a tab's component; `this` is then the tab, which
// reaches the page's `draft`, `errorFor` and `isInvalid` through Alpine scope.

import { addRow, pointer, removeRow, rowsAt, setList, toCsv } from "../../domain/descriptive.js";

/** @param {string[]} path the keys from the record to the list @param {() => any} blank a new entry */
export const listEditing = (path, blank) => ({
  rows() {
    return rowsAt(this.draft, path);
  },
  add() {
    addRow(this.draft, path, blank());
  },
  remove(index) {
    removeRow(this.draft, path, index);
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
});
