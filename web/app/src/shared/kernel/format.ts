const MINUTE = 60_000;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;

/** "now", "4m", "3h", "2d": compact, for list meta columns. The Alpine client's rule. */
export function relativeTime(then: Date, now: Date): string {
  const ms = Math.max(0, now.getTime() - then.getTime());
  if (ms < MINUTE) return "now";
  if (ms < HOUR) return `${Math.floor(ms / MINUTE)}m`;
  if (ms < DAY) return `${Math.floor(ms / HOUR)}h`;
  return `${Math.floor(ms / DAY)}d`;
}
