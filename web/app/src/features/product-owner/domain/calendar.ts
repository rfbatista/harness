// Dates and clock times of the digests, in the server's time zone. A digest's date is the plain
// calendar date "YYYY-MM-DD" of its digest time; a moment is shown as that zone's wall clock.
//
// The server names its zone by an IANA name, or by its offset ("-03:00") when it cannot tell the name; it also sends the next run, so nothing here computes one.

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

/** The offset in minutes east of UTC when the server names its zone by one ("+05:30", "-03:00"); null for an IANA name. */
function offsetOf(zone: string): number | null {
  const match = /^([+-])(\d{2}):?(\d{2})$/.exec(zone);
  return match ? (match[1] === "-" ? -1 : 1) * (Number(match[2]) * 60 + Number(match[3])) : null;
}

/** The wall clock of `at` in `timeZone`; a zone the browser does not know reads as the browser's own, so a page never fails on a name. */
function wall(at: Date, timeZone: string): Wall {
  const offset = offsetOf(timeZone);
  if (offset !== null) {
    const moved = new Date(at.getTime() + offset * 60000);
    return { year: moved.getUTCFullYear(), month: moved.getUTCMonth() + 1, day: moved.getUTCDate(), hour: moved.getUTCHours(), minute: moved.getUTCMinutes(), second: moved.getUTCSeconds() };
  }
  let parts: Intl.DateTimeFormatPart[];
  try {
    parts = new Intl.DateTimeFormat("en-GB", { timeZone, year: "numeric", month: "numeric", day: "numeric", hour: "numeric", minute: "numeric", second: "numeric", hourCycle: "h23" }).formatToParts(at);
  } catch (err) {
    if (!(err instanceof RangeError)) throw err;
    parts = new Intl.DateTimeFormat("en-GB", { year: "numeric", month: "numeric", day: "numeric", hour: "numeric", minute: "numeric", second: "numeric", hourCycle: "h23" }).formatToParts(at);
  }
  const read = (type: string): number => Number(parts.find((p) => p.type === type)?.value);
  return { year: read("year"), month: read("month"), day: read("day"), hour: read("hour"), minute: read("minute"), second: read("second") };
}

const two = (n: number): string => String(n).padStart(2, "0");

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
