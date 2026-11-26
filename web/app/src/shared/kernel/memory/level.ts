// The state of a session's memory use against its orçamento, with hysteresis and a hold, so a noisy
// sample every second does not make the word flicker. The first sample paints at once.

export type Level = "normal" | "near" | "over";

export const HOLD_MS = 3000;

const NEAR_ENTER = 0.8;
const NEAR_LEAVE = 0.75;
const OVER_ENTER = 1.0;
const OVER_LEAVE = 0.95;

export interface Tracker {
  readonly level: Level;
  /** When the level last changed, in ms. */
  readonly since: number;
}

export const initialTracker = (): Tracker => ({ level: "normal", since: Number.NEGATIVE_INFINITY });

/** The level `share` asks for from `current`, before the hold. */
function wanted(current: Level, share: number): Level {
  if (share >= (current === "over" ? OVER_LEAVE : OVER_ENTER)) return "over";
  if (share >= (current === "normal" ? NEAR_ENTER : NEAR_LEAVE)) return "near";
  return "normal";
}

/** A kill is a real change of state, not noise: the hold ends, so the next sample's level is taken as it is. */
export const releaseHold = (tracker: Tracker): Tracker => ({ level: tracker.level, since: Number.NEGATIVE_INFINITY });

/** The tracker after a sample of `share` (rss over budget) at `now`. */
export function settle(tracker: Tracker, share: number, now: number): Tracker {
  if (now - tracker.since < HOLD_MS) return tracker;
  const level = wanted(tracker.level, share);
  return level === tracker.level ? tracker : { level, since: now };
}
