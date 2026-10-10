// Lines of code: splitting content, line ranges from a URL or the Go to line
// field, the gutter's width, and the window a virtualised list renders.

/** Above this many lines a .code-lines block renders only its window. */
export const VIRTUALISE_FROM = 1_000;
/** Lines rendered beyond each edge of the viewport. */
export const OVERSCAN = 200;
/** Above this many lines a file cannot wrap (UX §8). */
export const WRAP_UP_TO = 5_000;

/**
 * A file's content as lines. A trailing newline ends the last line rather
 * than starting an empty one; "" has no lines.
 * @param {string|null} content
 * @returns {string[]}
 */
export function splitLines(content) {
  if (!content) return [];
  const lines = content.split("\n");
  if (lines.at(-1) === "") lines.pop();
  return lines.map((l) => (l.endsWith("\r") ? l.slice(0, -1) : l));
}

/** The digit count of the largest line number, for the gutter's --digits. */
export const digits = (largest) => Math.max(1, String(Math.max(0, Math.trunc(largest))).length);

/**
 * @typedef {{ start: number, end: number }} LineRange  1-based, inclusive, start <= end
 */

/**
 * Reads "120" or "120-140" (as typed in Go to line; spaces and a reversed
 * range are fine). null when it is not a line or a range.
 * @returns {LineRange|null}
 */
export function parseRange(text) {
  const m = /^\s*(\d+)\s*(?:[-–:]\s*(\d+))?\s*$/.exec(text ?? "");
  if (!m) return null;
  const a = Number(m[1]);
  const b = m[2] === undefined ? a : Number(m[2]);
  if (a < 1 || b < 1) return null;
  return { start: Math.min(a, b), end: Math.max(a, b) };
}

/**
 * Clamps a range to a file of `count` lines. The note says what changed,
 * for the person to read ("" when nothing did).
 * @returns {{ range: LineRange|null, note: string }}
 */
export function clampRange(range, count) {
  if (!range || count < 1) return { range: null, note: count < 1 ? "This file has no lines." : "" };
  const start = Math.min(range.start, count);
  const end = Math.min(range.end, count);
  const note = range.end > count ? `The file has ${count.toLocaleString("en-US")} lines; went to line ${end.toLocaleString("en-US")}.` : "";
  return { range: { start, end }, note };
}

/** "#L45" or "#L45-L49" for a range in a file. */
export function rangeHash(range) {
  if (!range) return "";
  return range.start === range.end ? `#L${range.start}` : `#L${range.start}-L${range.end}`;
}

/** The range a file hash targets: "#L45", "#L45-L49"; null otherwise. */
export function parseRangeHash(hash) {
  const m = /^#?L(\d+)(?:-L?(\d+))?$/.exec(hash ?? "");
  if (!m) return null;
  return parseRange(m[2] ? `${m[1]}-${m[2]}` : m[1]);
}

/** Whether line n is in the range. */
export const inRange = (range, n) => !!range && n >= range.start && n <= range.end;

/**
 * The lines a virtualised list renders: [first, last) as 0-based indexes,
 * for a list of `count` rows of `rowHeight` px whose top sits at `offset` px
 * inside a scroller showing [scrollTop, scrollTop + viewport).
 * @returns {{ first: number, last: number }}
 */
export function windowOf({ count, rowHeight, scrollTop, viewport, offset = 0, overscan = OVERSCAN }) {
  if (count <= 0 || rowHeight <= 0) return { first: 0, last: 0 };
  const top = Math.max(0, scrollTop - offset);
  const firstVisible = Math.floor(top / rowHeight);
  const lastVisible = Math.ceil((top + Math.max(0, viewport)) / rowHeight);
  const first = Math.max(0, Math.min(count, firstVisible - overscan));
  const last = Math.max(first, Math.min(count, lastVisible + overscan));
  return { first, last };
}

/** The scrollTop that puts row `index` (0-based) a third of the way down the viewport. */
export function scrollTopFor({ index, rowHeight, viewport, offset = 0 }) {
  return Math.max(0, offset + index * rowHeight - Math.floor(viewport / 3));
}
