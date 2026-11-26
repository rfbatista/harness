// The contract's memory values (Contract: memória da sessão), decoded.

export interface HeaviestProcess {
  pid: number;
  /** The process name and at most its first argument; never a full command line. */
  command: string;
  rssBytes: number;
}

export interface MemorySample {
  sessionId: string;
  taskId: string;
  /** null before the first sample of a session. */
  sampledAt: Date | null;
  rssBytes: number;
  budgetBytes: number;
  processes: number;
  heaviest: HeaviestProcess | null;
}

export type KillSignal = "SIGTERM" | "SIGKILL";

/** An encerramento por memória. */
export interface MemoryKill {
  sessionId: string;
  taskId: string;
  killedAt: Date;
  pid: number;
  command: string;
  rssBytes: number;
  treeRssBytes: number;
  budgetBytes: number;
  signal: KillSignal;
}

export type BudgetSource = "task" | "repository" | "global";

/** Chip: how long a kill reads as "ended". Card and bar: the same, longer. */
export const CHIP_ENDED_MS = 60_000;
export const CARD_ENDED_MS = 300_000;

/** A sample says something: not before the first sample and not the zero a session sends when it ends. */
export const hasValue = (sample: MemorySample | null): sample is MemorySample =>
  sample !== null && sample.sampledAt !== null && sample.rssBytes > 0;

/** The fraction of the orçamento in use. */
export const shareOf = (sample: MemorySample): number => (sample.budgetBytes > 0 ? sample.rssBytes / sample.budgetBytes : 0);

/** One kill, the same wherever it is seen (stream, replay, initial read). */
export const killKey = (kill: MemoryKill): string => `${kill.sessionId}:${kill.pid}:${kill.killedAt.getTime()}`;

/** The latest kill within `windowMs` of `now`, or null. */
export function endedBy(kills: readonly MemoryKill[], now: number, windowMs: number): MemoryKill | null {
  let latest: MemoryKill | null = null;
  for (const kill of kills) {
    const age = now - kill.killedAt.getTime();
    if (age < 0 || age >= windowMs) continue;
    if (latest === null || kill.killedAt > latest.killedAt) latest = kill;
  }
  return latest;
}
