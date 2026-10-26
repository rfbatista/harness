// Two editors share one page and one set of save-pipeline markup
// (agents/save.templ), which binds to fixed names: sv, fieldErrors, saveEditor …
// Both editors define those names; this gives the page the one of the editor
// that is open (`isOpen` says whether it is the second), and runs both for the
// members that must see every route change.

/** Members both editors answer to; the open one's wins. */
const SHARED = [
  "sv", "reviewFiles", "conflictFiles", "fieldErrors", "formInvalid", "refusalNext", "refusalCode", "saveButtonLabel",
  "editorHref", "entriesInvalid", "entriesDescribedBy", "saveEditor", "overwrite", "reloadEditor", "cancelEditor",
];
/** Members every route change must run on both. */
const BOTH = ["setupEditor", "syncEditor", "closeEditor"];

/**
 * @param {(page: any) => boolean} isOpen  whether `second` is the editor that is open
 * @param {object} first
 * @param {object} second
 */
export function editors(isOpen, first, second) {
  const a = Object.getOwnPropertyDescriptors(first);
  const b = Object.getOwnPropertyDescriptors(second);
  const out = {};
  for (const key of new Set([...Object.keys(a), ...Object.keys(b)])) {
    if (!(key in a)) out[key] = b[key];
    else if (!(key in b)) out[key] = a[key];
    else if (BOTH.includes(key)) {
      out[key] = { configurable: true, enumerable: true, writable: true, value() { a[key].value.apply(this); b[key].value.apply(this); } };
    } else if (SHARED.includes(key)) {
      const pick = (self) => (isOpen(self) ? b[key] : a[key]);
      out[key] = a[key].get
        ? { configurable: true, enumerable: true, get() { return pick(this).get.call(this); } }
        : { configurable: true, enumerable: true, writable: true, value(...args) { return pick(this).value.apply(this, args); } };
    } else {
      throw new Error(`both editors define ${key}, and it is not a shared member`);
    }
  }
  return Object.defineProperties({}, out);
}
