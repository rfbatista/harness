// The memory of a task's live sessions together, for the whiteboard bar.

import { formatShown } from "./bytes";
import type { Level } from "./level";
import { CARD_ENDED_MS } from "./sample";

export interface SessionMemory {
  /** The last value, null before a first sample. */
  rssBytes: number | null;
  level: Level;
  killedAt: readonly Date[];
}

export interface MemorySummary {
  /** The sum, null when no session has a value. */
  total: string | null;
  /** Sessions with a value. */
  sampled: number;
  near: number;
  /** Kills in the last 5 minutes. */
  ended: number;
}

export function summarize(sessions: readonly SessionMemory[], now: number): MemorySummary {
  let bytes = 0;
  let sampled = 0;
  let near = 0;
  let ended = 0;
  for (const s of sessions) {
    ended += s.killedAt.filter((at) => now - at.getTime() < CARD_ENDED_MS).length;
    if (s.rssBytes === null || s.rssBytes <= 0) continue;
    sampled++;
    bytes += s.rssBytes;
    if (s.level === "near") near++;
  }
  return { total: sampled === 0 ? null : formatShown(bytes), sampled, near, ended };
}
