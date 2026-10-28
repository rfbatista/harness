// The rows of code a .code-lines block draws, and their syntax tokens: pure
// functions, so the code worker (which prepares a diff page off the main
// thread) and the main thread (the Files view) share them. No DOM.

import { sidesOf, splitRows } from "./diff.js";

/**
 * @typedef {{ type: "line", n: number, text: string }} FileRow
 * @typedef {{ type: "hunk", range: string, header: string }} HunkRow
 * @typedef {{ type: "diff", kind: string, old: number|null, new: number|null, text: string, noNewline: boolean, tokens?: any }} DiffRow
 * @typedef {{ type: "split", left: DiffRow|null, right: DiffRow|null }} SplitRow
 * @typedef {FileRow|HunkRow|DiffRow|SplitRow} Row
 */

/** Rows of a file: one per line, numbered from 1. */
export function fileRows(lines) {
  return lines.map((text, i) => ({ type: "line", n: i + 1, text }));
}

const rangeOf = (h) => `@@ -${h.oldStart},${h.oldLines} +${h.newStart},${h.newLines} @@`;

/** Rows of a file diff's hunks, unified or split. */
export function diffRows(hunks, layout = "unified") {
  const rows = [];
  for (const h of hunks ?? []) {
    rows.push({ type: "hunk", range: rangeOf(h), header: h.header });
    if (layout === "split") {
      for (const { left, right } of splitRows(h.lines)) {
        const l = left && toDiffRow(left);
        // A context line is one row on both sides, so it is highlighted once.
        rows.push({ type: "split", left: l, right: right === left ? l : right && toDiffRow(right) });
      }
    } else {
      for (const l of h.lines) rows.push(toDiffRow(l));
    }
  }
  return rows;
}

const toDiffRow = (l) => ({ type: "diff", kind: l.kind, old: l.old, new: l.new, text: l.text, noNewline: l.noNewline });

/**
 * Tokens per row: a file's lines in one pass; a diff's hunks per side (the
 * new side for context and additions, the old side for deletions).
 * @param {Row[]} rows
 * @param {import("./ports.js").Highlighter} highlighter
 * @returns {Map<object, any>}
 */
export function tokenise(rows, highlighter, language) {
  const out = new Map();
  if (rows.every((r) => r.type === "line")) {
    const lines = highlighter.tokenize(language, rows.map((r) => r.text).join("\n"));
    rows.forEach((r, i) => out.set(r, lines[i] ?? null));
    return out;
  }
  // Group the diff rows back into hunks to highlight each side as text.
  let hunk = [];
  const flush = () => {
    if (!hunk.length) return;
    const lines = hunk.map((d) => ({ kind: d.kind, text: d.text }));
    const { newText, oldText, at } = sidesOf({ lines });
    const newT = highlighter.tokenize(language, newText);
    const oldT = highlighter.tokenize(language, oldText);
    hunk.forEach((d, i) => out.set(d, d.kind === "del" ? oldT[at[i].old] : newT[at[i].new]));
    hunk = [];
  };
  for (const r of rows) {
    if (r.type === "hunk") flush();
    else if (r.type === "diff") hunk.push(r);
    else if (r.type === "split") {
      // Old side then new side, in order, without repeating a context line.
      if (r.left) hunk.push(r.left);
      if (r.right && r.right !== r.left) hunk.push(r.right);
    }
  }
  flush();
  return out;
}

/**
 * The index of the row that shows a line on one side ("R" new, "L" old) of a
 * diff's rows, unified or split; for the new side the nearest row after it
 * when the line is not shown; -1 when absent.
 * @param {Row[]} rows
 */
export function rowOfLineIn(rows, side, n) {
  let after = -1;
  for (let i = 0; i < rows.length; i += 1) {
    const r = rows[i];
    const halves = r.type === "diff" ? [r] : r.type === "split" ? [side === "R" ? r.right : r.left] : [];
    for (const d of halves) {
      if (!d) continue;
      const line = side === "R" ? d.new : d.old;
      if (line === n) return i;
      if (side === "R" && after < 0 && line !== null && line > n) after = i;
    }
  }
  return side === "R" ? after : -1;
}
