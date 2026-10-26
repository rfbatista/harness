// What the four descriptive tabs share: editing one list of the draft.
// Spread the result into a tab's component; `this` is then the tab, which
// reaches the page's `draft`, `errors`, `errorFor` and `isInvalid` through Alpine
// scope. The tab declares the field named by `editKey` (-1): the position of the
// row whose editor is open.

import { addRow, pointer, removeRow, rowsAt, setList, toCsv } from "../../domain/descriptive.js";

/**
 * @param {string[]} path the keys from the record to the list @param {() => any} blank a new entry
 * @param {string} editKey the tab's field holding the position of the row being edited
 */
export const listEditing = (path, blank, editKey = "editing") => ({
  rows() {
    return rowsAt(this.draft, path);
  },
  /** Adds a row and opens its editor. */
  add() {
    addRow(this.draft, path, blank());
    this[editKey] = rowsAt(this.draft, path).length - 1;
  },
  remove(index) {
    removeRow(this.draft, path, index);
    if (this[editKey] === index) this[editKey] = -1;
    else if (this[editKey] > index) this[editKey] -= 1;
  },
  /** Whether the row's editor is shown: it was opened, or a save refused something in the row. */
  isOpen(index) {
    return this[editKey] === index || this.errors.some((e) => e.path.startsWith(pointer(path, index, "")));
  },
  /** The Edit button: opens the row's editor, or closes it when it is open. */
  edit(index) {
    this[editKey] = this[editKey] === index ? -1 : index;
  },
  closeEditor() {
    this[editKey] = -1;
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
});
