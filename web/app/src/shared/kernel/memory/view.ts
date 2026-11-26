// What the chip and the card say about a session's memory. The words are the design's.

import { formatBudget, formatShown, splitShown } from "./bytes";
import type { Level } from "./level";
import { hasValue, shareOf, type MemoryKill, type MemorySample } from "./sample";

export type MemoryState = Level | "ended";

export interface MemoryView {
  state: MemoryState;
  /** "3.4", or "905 MB" below 1 GB. */
  num: string;
  /** " / 4 GB" */
  of: string;
  /** "", "near budget", "over budget" or "{command} ended". */
  word: string;
  /** 0 to 100, the fill of the track. */
  fillPercent: number;
  /** The accessible sentence: title and aria-label. */
  title: string;
}

const WORDS: Record<Level, string> = { normal: "", near: "near budget", over: "over budget" };

/** The view of `sample` at `level`, or null when there is nothing to show. `ended` is the kill the chip is saying. */
export function memoryView(sample: MemorySample | null, level: Level, ended: MemoryKill | null): MemoryView | null {
  if (!hasValue(sample)) return null;
  const [number, unit] = splitShown(sample.rssBytes);
  const budget = formatBudget(sample.budgetBytes);
  const word = ended ? `${ended.command} ended` : WORDS[level];
  const processes = `${sample.processes} ${sample.processes === 1 ? "process" : "processes"}`;
  return {
    state: ended ? "ended" : level,
    num: unit === "MB" ? `${number} MB` : number,
    of: ` / ${budget}`,
    word,
    fillPercent: Math.min(100, Math.round(shareOf(sample) * 100)),
    title: `Memory: ${formatShown(sample.rssBytes)} of the ${budget} budget${word ? ` · ${word}` : ""} · ${processes}`,
  };
}
