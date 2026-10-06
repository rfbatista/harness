// Pure formatting shared by every module. The Go BFF has the same rules in
// its view.go files; web/testdata/views pins them together.

const MINUTE = 60_000;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;

/** "now", "4m", "3h", "2d" — compact, for list meta columns. */
export function relativeTime(then, now) {
  const ms = Math.max(0, now.getTime() - then.getTime());
  if (ms < MINUTE) return "now";
  if (ms < HOUR) return `${Math.floor(ms / MINUTE)}m`;
  if (ms < DAY) return `${Math.floor(ms / HOUR)}h`;
  return `${Math.floor(ms / DAY)}d`;
}

/** "1 session", "3 sessions". */
export function count(n, noun) {
  return `${n} ${noun}${n === 1 ? "" : "s"}`;
}

const UNITS = ["B", "KB", "MB", "GB", "TB"];

/** "0 B", "999 B", "1.5 KB", "3.0 MB" — a file size as a file manager shows it. */
export function bytes(n) {
  let value = Math.max(0, n);
  let unit = 0;
  while (value >= 1024 && unit < UNITS.length - 1) {
    value /= 1024;
    unit++;
  }
  return unit === 0 ? `${Math.round(value)} ${UNITS[0]}` : `${value.toFixed(1)} ${UNITS[unit]}`;
}
