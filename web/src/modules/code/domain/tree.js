// The file tree as a person moves through it: directories load one level at a
// time (contract §3 /tree), and the keys follow the WAI-ARIA APG tree pattern.
// State is plain data, so a component can keep it reactive:
//
//   levels:   { [dir]: TreeEntry[] }   loaded directories ("" is the root)
//   expanded: { [dir]: true }          open directories
//   busy:     { [dir]: true }          directories whose level is loading

import { ancestors } from "./code.js";

/**
 * @typedef {{ levels: Record<string, import("./code.js").TreeEntry[]>, expanded: Record<string, boolean>, busy: Record<string, boolean> }} TreeState
 * @typedef {{ entry: import("./code.js").TreeEntry, depth: number, parent: string }} TreeRow
 */

/** @returns {TreeState} */
export const emptyTree = () => ({ levels: {}, expanded: {}, busy: {} });

/** A directory that can be opened in place: not a symlinked directory, not a submodule. */
export const expandable = (entry) => entry.kind === "dir";

/**
 * What opening an entry does: a file opens, a deleted file opens its diff
 * (/file would be PATH_NOT_FOUND, C8), a symlink or submodule shows its
 * placeholder (C3), a directory toggles.
 * @returns {"file"|"diff"|"symlink"|"submodule"|"toggle"}
 */
export function openAction(entry) {
  if (entry.kind === "dir") return "toggle";
  if (entry.kind === "symlink") return "symlink";
  if (entry.kind === "submodule") return "submodule";
  return entry.change === "deleted" ? "diff" : "file";
}

/**
 * The rows on screen, in order: the root's entries and, under each open
 * directory, its loaded entries.
 * @param {TreeState} state
 * @returns {TreeRow[]}
 */
export function visibleRows(state) {
  const rows = [];
  const walk = (dir, depth) => {
    for (const entry of state.levels[dir] ?? []) {
      rows.push({ entry, depth, parent: dir });
      if (entry.kind === "dir" && state.expanded[entry.path]) walk(entry.path, depth + 1);
    }
  };
  walk("", 0);
  return rows;
}

/**
 * The directories to load and open so that path shows: every ancestor
 * (outermost first) that is not loaded yet.
 */
export function missingAncestors(state, path) {
  return ancestors(path).filter((dir) => !state.levels[dir]);
}

/** Opens every ancestor of path (a file opened from Find file or a URL). */
export function revealPath(state, path) {
  const expanded = { ...state.expanded };
  for (const dir of ancestors(path)) expanded[dir] = true;
  return { ...state, expanded };
}

/**
 * The state with one directory's level loaded (or re-read). Directories that
 * disappeared from it are closed, with everything under them.
 */
export function withLevel(state, dir, entries) {
  const levels = { ...state.levels, [dir]: entries };
  const expanded = { ...state.expanded };
  const busy = { ...state.busy };
  delete busy[dir];
  const previous = state.levels[dir] ?? [];
  const still = new Set(entries.filter((e) => e.kind === "dir").map((e) => e.path));
  for (const gone of previous.filter((e) => e.kind === "dir" && !still.has(e.path))) {
    for (const key of Object.keys(levels)) if (key === gone.path || key.startsWith(gone.path + "/")) delete levels[key];
    for (const key of Object.keys(expanded)) if (key === gone.path || key.startsWith(gone.path + "/")) delete expanded[key];
  }
  return { levels, expanded, busy };
}

export function withBusy(state, dir, on) {
  const busy = { ...state.busy };
  if (on) busy[dir] = true;
  else delete busy[dir];
  return { ...state, busy };
}

export function withExpanded(state, dir, on) {
  const expanded = { ...state.expanded };
  if (on) expanded[dir] = true;
  else delete expanded[dir];
  return { ...state, expanded };
}

/** The open directories whose levels are loaded: what a live re-read refreshes. */
export const loadedOpenDirs = (state) => ["", ...Object.keys(state.expanded).filter((d) => state.levels[d])];

/**
 * Where a tree key moves focus, or what it asks for, from the row at
 * `current` (a path; "" or unknown means the first row).
 * @param {TreeState} state
 * @param {string} current
 * @param {string} key  ArrowDown ArrowUp ArrowRight ArrowLeft Home End *
 * @returns {{ focus?: string, expand?: string, collapse?: string, expandAll?: string[] }}
 */
export function treeKey(state, current, key) {
  const rows = visibleRows(state);
  if (rows.length === 0) return {};
  let i = rows.findIndex((r) => r.entry.path === current);
  if (i < 0) return { focus: rows[0].entry.path };
  const row = rows[i];
  const isDir = expandable(row.entry);
  const open = isDir && !!state.expanded[row.entry.path];
  switch (key) {
    case "ArrowDown":
      return { focus: rows[Math.min(rows.length - 1, i + 1)].entry.path };
    case "ArrowUp":
      return { focus: rows[Math.max(0, i - 1)].entry.path };
    case "Home":
      return { focus: rows[0].entry.path };
    case "End":
      return { focus: rows.at(-1).entry.path };
    case "ArrowRight":
      if (isDir && !open) return { expand: row.entry.path };
      if (open && rows[i + 1]?.parent === row.entry.path) return { focus: rows[i + 1].entry.path };
      return {};
    case "ArrowLeft":
      if (open) return { collapse: row.entry.path };
      return row.parent ? { focus: row.parent } : {};
    case "*":
      return { expandAll: rows.filter((r) => r.parent === row.parent && expandable(r.entry) && !state.expanded[r.entry.path]).map((r) => r.entry.path) };
    default:
      return {};
  }
}

/**
 * Type-ahead: the next visible row after current whose name starts with
 * the typed text (any case), wrapping; null when none does.
 */
export function typeAhead(state, current, typed) {
  const rows = visibleRows(state);
  if (!typed || rows.length === 0) return null;
  const t = typed.toLowerCase();
  const start = rows.findIndex((r) => r.entry.path === current);
  // A repeated single letter cycles; a longer prefix may stay on the current row.
  const from = typed.length > 1 && start >= 0 ? start : start + 1;
  for (let k = 0; k < rows.length; k += 1) {
    const r = rows[(from + k) % rows.length];
    if (r.entry.name.toLowerCase().startsWith(t)) return r.entry.path;
  }
  return null;
}
