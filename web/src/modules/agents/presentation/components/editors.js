// Several editors share one page and one set of save-pipeline markup
// (agents/save.templ), which binds to fixed names: sv, fieldErrors, saveEditor …
// Each editor defines those names; this gives the page the one of the editor
// that is open, and runs every editor for the members that must see each route
// change.

/** Members editors answer to alike; the open one's wins, else the first's. */
const SHARED = [
  "sv", "reviewFiles", "conflictFiles", "fieldErrors", "formInvalid", "refusalNext", "refusalCode", "saveButtonLabel",
  "editorHref", "entriesInvalid", "entriesDescribedBy", "saveEditor", "overwrite", "reloadEditor", "cancelEditor",
];
/** Members every route change must run on all of them. */
const BOTH = ["setupEditor", "syncEditor", "closeEditor"];

/**
 * @param {{ part: object, isOpen?: (page: any) => boolean }[]} editors  the first is the default; the others say when they are the open one
 */
export function editors(...editors) {
  const parts = editors.map((e) => ({ isOpen: e.isOpen ?? (() => false), descriptors: Object.getOwnPropertyDescriptors(e.part) }));
  const out = {};
  const keys = new Set(parts.flatMap((p) => Object.keys(p.descriptors)));
  for (const key of keys) {
    const holders = parts.filter((p) => key in p.descriptors);
    if (holders.length === 1) {
      out[key] = holders[0].descriptors[key];
    } else if (BOTH.includes(key)) {
      out[key] = { configurable: true, enumerable: true, writable: true, value() { for (const h of holders) h.descriptors[key].value.apply(this); } };
    } else if (SHARED.includes(key)) {
      const pick = (self) => (holders.find((h) => h.isOpen(self)) ?? holders[0]).descriptors[key];
      out[key] = holders[0].descriptors[key].get
        ? { configurable: true, enumerable: true, get() { return pick(this).get.call(this); } }
        : { configurable: true, enumerable: true, writable: true, value(...args) { return pick(this).value.apply(this, args); } };
    } else {
      throw new Error(`more than one editor defines ${key}, and it is not a shared member`);
    }
  }
  return Object.defineProperties({}, out);
}
