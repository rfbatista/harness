// How the Score page writes its figures.

import type { Delta } from "./comparison";

/** A score to two places; a case with nothing scored is a dash. */
export const score = (n: number | null): string => (n === null ? "—" : n.toFixed(2));

export function cost(usd: number): string {
  return `$${usd.toFixed(2)}`;
}

export function duration(ms: number): string {
  const s = Math.round(ms / 1000);
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ${String(s % 60).padStart(2, "0")}s`;
  return `${Math.floor(m / 60)}h ${String(m % 60).padStart(2, "0")}m`;
}

const pad = (n: number): string => String(n).padStart(2, "0");
const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

/** "today 11:40", "yesterday 18:02" or "Nov 24 18:02", in the viewer's time zone. */
export function when(at: Date, now: Date): string {
  const hhmm = `${pad(at.getHours())}:${pad(at.getMinutes())}`;
  const day = (d: Date): number => new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
  const days = Math.round((day(now) - day(at)) / 86_400_000);
  if (days === 0) return `today ${hhmm}`;
  if (days === 1) return `yesterday ${hhmm}`;
  return `${MONTHS[at.getMonth()]} ${at.getDate()} ${hhmm}`;
}

/** The sign and the number of a delta: "▼ 0.24"; equal "=", a first run "first run", a new case "new". */
export function deltaShort(d: Delta): string {
  switch (d.direction) {
    case "up":
      return `▲ ${d.change.toFixed(2)}`;
    case "down":
      return `▼ ${d.change.toFixed(2)}`;
    case "first":
      return "first run";
    case "new":
      return "new";
    default:
      return "=";
  }
}
