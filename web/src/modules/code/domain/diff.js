// Diffs: the anchors of their files and lines, the words for what was
// withheld, the git command to read a withheld file locally, the rows of a
// split layout, and which files start collapsed.

import { LIMITS, formatCount, formatSize, shortHash } from "./code.js";

/** A file diff's anchor stem: "internal/feed/feed.go" → "internal-feed-feed-go" (UX §2.4). */
export const slugOf = (path) => path.replace(/[^A-Za-z0-9_]/g, "-");

/**
 * Each file's slug, unique within the diff: a repeat takes -2, -3 in path order.
 * @param {string[]} paths
 * @returns {Map<string, string>} path → slug
 */
export function slugs(paths) {
  const seen = new Map();
  const out = new Map();
  for (const path of paths) {
    const base = slugOf(path);
    const n = (seen.get(base) ?? 0) + 1;
    seen.set(base, n);
    out.set(path, n === 1 ? base : `${base}-${n}`);
  }
  return out;
}

/** "#diff-{slug}-R12" (new side) or "-L12" (old side). */
export const lineAnchor = (slug, side, n) => `diff-${slug}-${side}${n}`;

/**
 * What a diff hash targets: a file, and maybe a line on one side.
 * "#diff-internal-feed-go-R12" → { slug: "internal-feed-go", side: "R", line: 12 }
 * @returns {{ slug: string, side: "R"|"L"|"", line: number } | null}
 */
export function parseDiffHash(hash) {
  const h = (hash ?? "").replace(/^#/, "");
  if (!h.startsWith("diff-")) return null;
  const rest = h.slice(5);
  const m = /^(.*)-([RL])(\d+)$/.exec(rest);
  if (m && m[1]) return { slug: m[1], side: /** @type {"R"|"L"} */ (m[2]), line: Number(m[3]) };
  return rest ? { slug: rest, side: "", line: 0 } : null;
}

/** Whether a file's counts can be printed (binary files carry −1). */
export const hasCounts = (f) => !f.binary && f.added >= 0 && f.deleted >= 0;

/** Changed lines of a file, as the limits count them. */
export const changedLines = (f) => (hasCounts(f) ? f.added + f.deleted : 0);

/** A deleted file over 50 lines starts collapsed (UX §5). */
export const startsCollapsed = (f) => f.status === "deleted" && f.deleted > 50;

/** Every file's hunks were withheld from by the totals limit. */
export const notLoaded = (f) => f.withheld === "too_large";

/**
 * The words in place of a withheld file's hunks (UX §5).
 * @param {import("./code.js").FileDiff} f
 * @param {{ size?: number, asked?: boolean }} [opts]  asked: the file was already fetched with path=
 * @returns {{ reason: string, title: string, detail: string, action: string } | null}
 */
export function withheldCopy(f, { size, asked = false } = {}) {
  if (f.withheld === "binary" || (f.binary && !f.hunks)) {
    return {
      reason: "binary",
      title: "Binary file",
      detail: `${size ? `${formatSize(size)}. ` : ""}Its changes are not shown as text.`,
      action: "",
    };
  }
  if (f.withheld !== "too_large") return null;
  const lines = changedLines(f);
  if (asked || lines > LIMITS.pathLines) {
    return {
      reason: "too_large",
      title: "Too large to show here",
      detail: lines > LIMITS.pathLines ? `Over ${formatCount(LIMITS.pathLines)} changed lines.` : "The patch is over 4 MB.",
      action: "view",
    };
  }
  return {
    reason: "too_large",
    title: "Large diff, not loaded",
    detail: `${formatCount(lines)} changed lines. Loading it may slow this page.`,
    action: "load",
  };
}

/**
 * The command that shows a withheld file's diff in a local clone.
 * @param {{ scope: string, base: string, head: string, path: string }} d
 */
export function gitCommand({ scope, base, head, path }) {
  const p = quotePath(path);
  if (scope === "commit") return `git show ${shortHash(head)} -- ${p}`;
  if (scope === "working") return `git diff HEAD -- ${p}`;
  if (scope === "all") return `git diff ${shortHash(base)} -- ${p}`;
  return `git diff ${shortHash(base)}...${shortHash(head)} -- ${p}`;
}

const quotePath = (p) => (/^[A-Za-z0-9_./@+-]+$/.test(p) ? p : `'${p.replace(/'/g, `'\\''`)}'`);

/** The ⁠+N −M of a file head or a summary row; null for binary. */
export function statOf(f) {
  return hasCounts(f) ? { added: `+${formatCount(f.added)}`, deleted: `−${formatCount(f.deleted)}` } : null;
}

/** "12 files" with "+214 −38" totals for the summary head. */
export function totals(diff) {
  return {
    files: `${formatCount(diff.files.length)} ${diff.files.length === 1 ? "file" : "files"}`,
    added: `+${formatCount(diff.added)}`,
    deleted: `−${formatCount(diff.deleted)}`,
  };
}

/**
 * The rows of a split layout for one hunk: each run of deletions beside the
 * run of additions that follows it, context on both sides, and empty halves
 * where one side has fewer lines.
 * @param {import("./code.js").HunkLine[]} lines
 * @returns {{ left: import("./code.js").HunkLine|null, right: import("./code.js").HunkLine|null }[]}
 */
export function splitRows(lines) {
  const rows = [];
  let i = 0;
  while (i < lines.length) {
    const line = lines[i];
    if (line.kind === "context") {
      rows.push({ left: line, right: line });
      i += 1;
      continue;
    }
    const dels = [];
    const adds = [];
    while (i < lines.length && lines[i].kind === "del") dels.push(lines[i++]);
    while (i < lines.length && lines[i].kind === "add") adds.push(lines[i++]);
    const n = Math.max(dels.length, adds.length);
    for (let k = 0; k < n; k += 1) rows.push({ left: dels[k] ?? null, right: adds[k] ?? null });
  }
  return rows;
}

/** The largest line number a file's hunks print, for the gutter. */
export function largestLine(hunks) {
  let max = 0;
  for (const h of hunks ?? []) max = Math.max(max, h.oldStart + h.oldLines, h.newStart + h.newLines);
  return max;
}

/** Lines in a file's hunks, hunk header rows included: what a unified layout renders. */
export const unifiedRowCount = (hunks) => (hunks ?? []).reduce((n, h) => n + 1 + h.lines.length, 0);

/**
 * The index of the unified row (hunk headers counted) that shows new-side
 * line n, or the nearest one after it; -1 when the file shows no such line.
 */
export function rowOfNewLine(hunks, n) {
  let row = 0;
  let after = -1;
  for (const h of hunks ?? []) {
    row += 1;
    for (const line of h.lines) {
      if (line.new === n) return row;
      if (after < 0 && line.new !== null && line.new > n) after = row;
      row += 1;
    }
  }
  return after;
}

/** The index of the unified row showing a line on one side ("R" new, "L" old); -1 when absent. */
export function rowOfLine(hunks, side, n) {
  if (side === "R") return rowOfNewLine(hunks, n);
  let row = 0;
  for (const h of hunks ?? []) {
    row += 1;
    for (const line of h.lines) {
      if (line.old === n) return row;
      row += 1;
    }
  }
  return -1;
}

/** The new side's text of a hunk, the old side's, and which line of each a hunk line is. */
export function sidesOf(hunk) {
  const newSide = [];
  const oldSide = [];
  const at = hunk.lines.map((line) => {
    const pos = { old: -1, new: -1 };
    if (line.kind !== "add") pos.old = oldSide.push(line.text) - 1;
    if (line.kind !== "del") pos.new = newSide.push(line.text) - 1;
    return pos;
  });
  return { newText: newSide.join("\n"), oldText: oldSide.join("\n"), at };
}
