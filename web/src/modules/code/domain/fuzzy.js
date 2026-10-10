// Find file's fuzzy match: the query's characters in order anywhere in the
// path, any case. A match inside the file name ranks first, then runs of
// consecutive characters and matches at word starts; shorter paths break ties.

/**
 * @typedef {{ path: string, score: number, ranges: [number, number][] }} Match
 *   ranges: [start, end) character spans of the path that matched, for <mark>
 */

const SEPARATORS = "/_-. ";

function isWordStart(path, i) {
  if (i === 0) return true;
  const prev = path[i - 1];
  if (SEPARATORS.includes(prev)) return true;
  const c = path[i];
  return prev === prev.toLowerCase() && c !== c.toLowerCase();
}

/**
 * Greedy left-to-right placement of q in lower (from `from`), preferring a
 * word start when one comes before the next plain occurrence is used up.
 * @returns {number[]|null} the matched indexes
 */
function place(path, lower, q, from) {
  const at = [];
  let i = from;
  for (const ch of q) {
    const next = lower.indexOf(ch, i);
    if (next < 0) return null;
    at.push(next);
    i = next + 1;
  }
  return at;
}

function scoreOf(path, at, nameStart) {
  let score = 0;
  for (let k = 0; k < at.length; k += 1) {
    const i = at[k];
    score += 1;
    if (k > 0 && at[k - 1] === i - 1) score += 5;
    if (isWordStart(path, i)) score += 3;
    if (i >= nameStart) score += 2;
  }
  if (at[0] >= nameStart) score += 10;
  score -= (at.at(-1) - at[0]) * 0.05;
  score -= path.length * 0.01;
  return score;
}

function toRanges(at) {
  const ranges = [];
  for (const i of at) {
    const last = ranges.at(-1);
    if (last && last[1] === i) last[1] = i + 1;
    else ranges.push([i, i + 1]);
  }
  return ranges;
}

/**
 * One path against a query; null when it does not match.
 * @returns {Match|null}
 */
export function matchPath(path, query) {
  const q = query.toLowerCase().replace(/\s+/g, "");
  if (!q) return { path, score: 0, ranges: [] };
  const lower = path.toLowerCase();
  const nameStart = path.lastIndexOf("/") + 1;
  // Prefer the whole query inside the file name, then anywhere.
  const inName = place(path, lower, q, nameStart);
  const at = inName ?? place(path, lower, q, 0);
  if (!at) return null;
  return { path, score: scoreOf(path, at, nameStart), ranges: toRanges(at) };
}

/**
 * The best `limit` matches of a query among paths, best first. An empty
 * query keeps the paths' order.
 * @param {string[]} paths
 * @returns {{ matches: Match[], total: number }}
 */
export function rank(paths, query, limit = 100) {
  const q = query.trim();
  if (!q) return { matches: paths.slice(0, limit).map((path) => ({ path, score: 0, ranges: [] })), total: paths.length };
  const found = [];
  for (const p of paths) {
    const m = matchPath(p, q);
    if (m) found.push(m);
  }
  found.sort((a, b) => b.score - a.score || a.path.length - b.path.length || (a.path < b.path ? -1 : 1));
  return { matches: found.slice(0, limit), total: found.length };
}

/**
 * Splits text into parts for rendering, marking the matched spans.
 * @param {string} text
 * @param {[number, number][]} ranges  relative to text
 * @returns {{ text: string, mark: boolean }[]}
 */
export function markParts(text, ranges) {
  const parts = [];
  let i = 0;
  for (const [s, e] of ranges) {
    const start = Math.max(i, Math.min(s, text.length));
    const end = Math.min(e, text.length);
    if (start > i) parts.push({ text: text.slice(i, start), mark: false });
    if (end > start) parts.push({ text: text.slice(start, end), mark: true });
    i = Math.max(i, end);
  }
  if (i < text.length) parts.push({ text: text.slice(i), mark: false });
  return parts;
}

/** Shifts ranges of a whole path to a slice of it starting at `offset`, dropping what falls outside. */
export function shiftRanges(ranges, offset, length) {
  return ranges
    .map(([s, e]) => [Math.max(0, s - offset), Math.min(length, e - offset)])
    .filter(([s, e]) => e > s);
}
