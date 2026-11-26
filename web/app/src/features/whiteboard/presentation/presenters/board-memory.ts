// What the board knows of each session's memory: the last sample and its state (with the hold), and the kills
// seen since the page opened. The rules are the shared kernel's; this keeps them per session.

import { initialTracker, levelNow, settle, type Level, type Tracker } from "../../../../shared/kernel/memory/level";
import { CARD_ENDED_MS, hasValue, killKey, shareOf } from "../../../../shared/kernel/memory/sample";
import { memoryView, type MemoryState } from "../../../../shared/kernel/memory/view";
import { summarize, type SessionMemory } from "../../../../shared/kernel/memory/summary";
import { relativeTime } from "../../../../shared/kernel/format";
import type { BoardMemory, BoardMemoryKill } from "../../domain/memory";

export interface CardMemory {
  state: MemoryState;
  /** "3.4", or "905 MB" below 1 GB. */
  num: string;
  /** " / 4 GB" */
  of: string;
  /** "", "near budget" or "over budget". */
  word: string;
  fillPercent: number;
  /** "Memory: 3.4 GB of the 4 GB budget · near budget · 7 processes" */
  sentence: string;
  /** The compact card's badge: "near budget", "over budget", "process ended", or "". */
  badge: string;
  /** For 5 minutes after a kill: the process and how long ago. */
  ended: { command: string; age: string } | null;
}

/** What the bar says about the task's live sessions together. */
export interface BarMemory {
  live: number;
  /** "10.7 GB"; null when no live session has a sample. */
  total: string | null;
  near: number;
  over: number;
  /** Processes ended in the last 5 minutes. */
  ended: number;
  /** "1 process ended", "2 processes ended". */
  endedLabel: string;
  /** "Memory: 10.7 GB across 4 live, 1 near budget, 1 process ended." */
  sentence: string;
}

interface Entry {
  sample: BoardMemory | null;
  tracker: Tracker;
  /** Kills seen live, with when this page saw them. */
  kills: { kill: BoardMemoryKill; seenAt: number }[];
}

const BADGES = { normal: "", near: "near budget", over: "over budget", ended: "process ended" } as const;

export class BoardMemoryState {
  readonly #entries = new Map<string, Entry>();

  #entry(id: string): Entry {
    let entry = this.#entries.get(id);
    if (!entry) this.#entries.set(id, (entry = { sample: null, tracker: initialTracker(), kills: [] }));
    return entry;
  }

  /** Takes `sample` unless the one it has is newer. */
  sample(sample: BoardMemory | null, now: number): void {
    if (!sample) return;
    const entry = this.#entry(sample.sessionId);
    const have = entry.sample?.sampledAt;
    if (have && (!sample.sampledAt || sample.sampledAt < have)) return;
    entry.sample = sample;
    entry.tracker = hasValue(sample) ? settle(entry.tracker, shareOf(sample), now) : initialTracker();
  }

  /** Notes a kill; false when it was already noted (a feed may deliver it twice), so nothing about it restarts. */
  kill(kill: BoardMemoryKill, now: number): boolean {
    const entry = this.#entry(kill.sessionId);
    if (entry.kills.some((k) => killKey(k.kill) === killKey(kill))) return false;
    entry.kills.push({ kill, seenAt: now });
    return true;
  }

  forget(id: string): void {
    this.#entries.delete(id);
  }

  /** The memory of `ids` together, for the bar. */
  summary(ids: readonly string[], now: number): BarMemory | null {
    const sessions: SessionMemory[] = ids.map((id) => {
      const entry = this.#entries.get(id);
      return { rssBytes: entry && hasValue(entry.sample) ? entry.sample.rssBytes : null, level: entry ? this.#level(entry) : "normal", killedAt: (entry?.kills ?? []).map((k) => new Date(k.seenAt)) };
    });
    const sum = summarize(sessions, now);
    if (sum.sampled === 0 && sum.ended === 0) return null;
    const endedLabel = `${sum.ended} ${sum.ended === 1 ? "process" : "processes"} ended`;
    const parts = [`${sum.total ?? "0 MB"} across ${ids.length} live`, ...(sum.near > 0 ? [`${sum.near} near budget`] : []), ...(sum.over > 0 ? [`${sum.over} over budget`] : []), ...(sum.ended > 0 ? [endedLabel] : [])];
    return { live: ids.length, total: sum.total, near: sum.near, over: sum.over, ended: sum.ended, endedLabel, sentence: `Memory: ${parts.join(", ")}.` };
  }

  /** The level shown: the held one, unless the last sample is already below what it needs. */
  #level(entry: Entry): Level {
    return hasValue(entry.sample) ? levelNow(entry.tracker.level, shareOf(entry.sample)) : "normal";
  }

  /** What the card says about the session's memory, or null when it says nothing. */
  of(id: string, now: number): CardMemory | null {
    const entry = this.#entries.get(id);
    if (!entry) return null;
    const ended = entry.kills.filter((k) => now - k.seenAt < CARD_ENDED_MS).reduce<{ kill: BoardMemoryKill; seenAt: number } | null>((latest, k) => (latest && latest.seenAt > k.seenAt ? latest : k), null);
    const view = memoryView(entry.sample, this.#level(entry), ended?.kill ?? null);
    if (!view) return null;
    return {
      state: view.state,
      num: view.num,
      of: view.of,
      word: ended ? "" : view.word,
      fillPercent: view.fillPercent,
      sentence: view.title,
      badge: BADGES[view.state],
      ended: ended ? { command: ended.kill.command, age: relativeTime(new Date(ended.seenAt), new Date(now)) } : null,
    };
  }
}
