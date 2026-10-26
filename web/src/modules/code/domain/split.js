// The split between the file list and the diff in a code layout, in percent
// of the layout's width. The list is at least 12rem and at most half of it.
// With no choice made the list has the CSS default: a third of the width,
// between 14rem and 18rem (code-layout.css).

const MIN_REM = 12;
const MAX_PERCENT = 50;
const DEFAULT_MIN_REM = 14;
const DEFAULT_MAX_REM = 18;
const DEFAULT_PERCENT = 32;
const STEP = 2;

const round1 = (n) => Math.round(n * 10) / 10;

/** The smallest and the largest percent the list may take in a layout `width` px wide. */
export function splitBounds(width, rem = 16) {
  const max = MAX_PERCENT;
  const min = Math.min(max, round1(((MIN_REM * rem) / width) * 100));
  return { min, max };
}

export function clampSplit(percent, width, rem = 16) {
  const { min, max } = splitBounds(width, rem);
  return Math.min(max, Math.max(min, round1(percent)));
}

/** What the list takes when no split was chosen. */
export function defaultSplit(width, rem = 16) {
  const lo = ((DEFAULT_MIN_REM * rem) / width) * 100;
  const hi = ((DEFAULT_MAX_REM * rem) / width) * 100;
  return round1(Math.min(hi, Math.max(lo, DEFAULT_PERCENT)));
}

/**
 * The split after a key on the separator: the new percent, null for Home (no
 * choice: the default again), undefined for a key it does not own.
 * @param {number} current
 * @param {string} key
 */
export function stepSplit(current, key, width, rem = 16) {
  if (key === "Home") return null;
  if (key === "ArrowLeft") return clampSplit(current - STEP, width, rem);
  if (key === "ArrowRight") return clampSplit(current + STEP, width, rem);
  return undefined;
}

/** A stored value back: a number of percent above 0 and up to 100, else no choice. */
export function parseSplit(raw) {
  if (raw === null || raw === undefined || raw === "") return null;
  const n = Number(raw);
  return Number.isFinite(n) && n > 0 && n <= 100 ? n : null;
}
