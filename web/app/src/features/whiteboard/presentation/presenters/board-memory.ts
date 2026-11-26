// What the board knows of each session's memory: the last sample and its state (with the hold), and the kills
// seen since the page opened. The rules are the shared kernel's; this keeps them per session.

import { initialTracker, settle, type Tracker } from "../../../../shared/kernel/memory/level";
import { CARD_ENDED_MS, hasValue, shareOf } from "../../../../shared/kernel/memory/sample";
import { memoryView, type MemoryState } from "../../../../shared/kernel/memory/view";
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

  kill(kill: BoardMemoryKill, now: number): void {
    this.#entry(kill.sessionId).kills.push({ kill, seenAt: now });
  }

  forget(id: string): void {
    this.#entries.delete(id);
  }

  /** What the card says about the session's memory, or null when it says nothing. */
  of(id: string, now: number): CardMemory | null {
    const entry = this.#entries.get(id);
    if (!entry) return null;
    const ended = entry.kills.filter((k) => now - k.seenAt < CARD_ENDED_MS).reduce<{ kill: BoardMemoryKill; seenAt: number } | null>((latest, k) => (latest && latest.seenAt > k.seenAt ? latest : k), null);
    const view = memoryView(entry.sample, entry.tracker.level, ended?.kill ?? null);
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
