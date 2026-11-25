// Dates and clock times of the digests, in the server's time zone. A digest's date is the plain
// calendar date "YYYY-MM-DD" of its digest time; a moment is shown as that zone's wall clock.
//
// The server names its zone as its clock does (Go's abbreviation: "-03", "CET", "UTC"), and an
// IANA name when it has one. An IANA name goes through Intl; a numeric offset or a common
// abbreviation is a fixed offset, which is wrong only across a change of daylight saving time.

const WEEKDAYS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];
const WEEKDAYS_LONG = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"];
const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];
const MONTHS_LONG = ["January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"];
const DAY = 24 * 60 * 60 * 1000;

interface Wall {
  readonly year: number;
  /** 1 to 12. */
  readonly month: number;
  readonly day: number;
  readonly hour: number;
  readonly minute: number;
  readonly second: number;
}

const ABBREVIATIONS: Readonly<Record<string, number>> = { UTC: 0, GMT: 0, Z: 0, WET: 0, WEST: 60, BST: 60, CET: 60, CEST: 120, EET: 120, EEST: 180, EST: -300, EDT: -240, CST: -360, CDT: -300, MST: -420, MDT: -360, PST: -480, PDT: -420, BRT: -180, IST: 330, JST: 540, AEST: 600, AEDT: 660 };

/** The offset in minutes east of UTC when the zone is named by one ("-03", "+0530", "+05:30") or by a common abbreviation; null for any other name. */
function fixedOffset(zone: string): number | null {
  const numeric = /^([+-])(\d{2})(?::?(\d{2}))?$/.exec(zone);
  if (numeric) return (numeric[1] === "-" ? -1 : 1) * (Number(numeric[2]) * 60 + Number(numeric[3] ?? 0));
  return ABBREVIATIONS[zone.toUpperCase()] ?? null;
}

/** The wall clock of `at` in `timeZone`; a RangeError for a zone that does not exist. */
function strictWall(at: Date, timeZone: string | undefined): Wall {
  const offset = timeZone === undefined ? null : fixedOffset(timeZone);
  if (offset !== null) {
    const moved = new Date(at.getTime() + offset * 60000);
    return { year: moved.getUTCFullYear(), month: moved.getUTCMonth() + 1, day: moved.getUTCDate(), hour: moved.getUTCHours(), minute: moved.getUTCMinutes(), second: moved.getUTCSeconds() };
  }
  const parts = new Intl.DateTimeFormat("en-GB", { timeZone, year: "numeric", month: "numeric", day: "numeric", hour: "numeric", minute: "numeric", second: "numeric", hourCycle: "h23" }).formatToParts(at);
  const read = (type: string): number => Number(parts.find((p) => p.type === type)?.value);
  return { year: read("year"), month: read("month"), day: read("day"), hour: read("hour"), minute: read("minute"), second: read("second") };
}

/** The wall clock to show: a zone that is not known reads as the browser's own, so a page never fails on a name. */
function wall(at: Date, timeZone: string): Wall {
  try {
    return strictWall(at, timeZone);
  } catch (err) {
    if (err instanceof RangeError) return strictWall(at, undefined);
    throw err;
  }
}

const asUtc = (w: Wall): number => Date.UTC(w.year, w.month - 1, w.day, w.hour, w.minute, w.second);

/** The instant at which `timeZone`'s clock reads `year-month-day hour:minute`. */
function instantOf(year: number, month: number, day: number, hour: number, minute: number, timeZone: string): number {
  const guess = Date.UTC(year, month - 1, day, hour, minute);
  const first = guess - (asUtc(strictWall(new Date(guess), timeZone)) - guess);
  // Across a change of offset the first guess is off by the change: read the offset again at the answer.
  return guess - (asUtc(strictWall(new Date(first), timeZone)) - first);
}

const two = (n: number): string => String(n).padStart(2, "0");

/** The next moment after `now` at which `timeZone`'s clock reads `time` ("HH:MM"); null when the time or the zone is unreadable. */
export function nextRun(now: Date, time: string, timeZone: string): Date | null {
  const match = /^([01]\d|2[0-3]):([0-5]\d)$/.exec(time);
  if (!match) return null;
  const hour = Number(match[1]);
  const minute = Number(match[2]);
  try {
    const today = strictWall(now, timeZone);
    const todays = instantOf(today.year, today.month, today.day, hour, minute, timeZone);
    if (todays > now.getTime()) return new Date(todays);
    const tomorrow = new Date(Date.UTC(today.year, today.month - 1, today.day + 1));
    return new Date(instantOf(tomorrow.getUTCFullYear(), tomorrow.getUTCMonth() + 1, tomorrow.getUTCDate(), hour, minute, timeZone));
  } catch (err) {
    if (err instanceof RangeError) return null;
    throw err;
  }
}

/** "09:00", the wall clock of `at` in `timeZone`. */
export function clockOf(at: Date, timeZone: string): string {
  const w = wall(at, timeZone);
  return `${two(w.hour)}:${two(w.minute)}`;
}

/** "Tue 24 Nov, 09:00". */
export function whenOf(at: Date, timeZone: string): string {
  const w = wall(at, timeZone);
  return `${dayLabel(w.year, w.month, w.day)}, ${two(w.hour)}:${two(w.minute)}`;
}

/** "Fri 20 Nov 09:00": where a digest's period starts and ends. */
export function stampOf(at: Date, timeZone: string): string {
  const w = wall(at, timeZone);
  return `${dayLabel(w.year, w.month, w.day)} ${two(w.hour)}:${two(w.minute)}`;
}

const dayLabel = (year: number, month: number, day: number): string => `${WEEKDAYS[new Date(Date.UTC(year, month - 1, day)).getUTCDay()]} ${day} ${MONTHS[month - 1]}`;

const parse = (date: string): { year: number; month: number; day: number } => {
  const [year, month, day] = date.split("-").map(Number) as [number, number, number];
  return { year, month, day };
};

/** "Mon 23 Nov" for "2026-11-23". */
export function shortDay(date: string): string {
  const { year, month, day } = parse(date);
  return dayLabel(year, month, day);
}

/** "Monday 23 November". */
export function longDay(date: string): string {
  const { year, month, day } = parse(date);
  return `${WEEKDAYS_LONG[new Date(Date.UTC(year, month - 1, day)).getUTCDay()]} ${day} ${MONTHS_LONG[month - 1]}`;
}

/** "November 2026". */
export function monthOf(date: string): string {
  const { year, month } = parse(date);
  return `${MONTHS_LONG[month - 1]} ${year}`;
}

/** The whole days from `earlier` to `later`, both "YYYY-MM-DD". */
export function daysBetween(earlier: string, later: string): number {
  const a = parse(earlier);
  const b = parse(later);
  return Math.round((Date.UTC(b.year, b.month - 1, b.day) - Date.UTC(a.year, a.month - 1, a.day)) / DAY);
}

/** `date` moved by `days`. */
export function addDays(date: string, days: number): string {
  const { year, month, day } = parse(date);
  return new Date(Date.UTC(year, month - 1, day + days)).toISOString().slice(0, 10);
}
