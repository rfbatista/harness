// What the Score page needs from the harness: the project's cases and scorecards, a run to start, and the feed that follows it.

import type { BenchCase, BenchRun, RunChange } from "../domain/bench";
import type { Scorecard } from "../domain/scorecard";

export interface ScoreSource {
  /** The project's bench cases. Fails with a ScoreError. */
  cases(projectId: string, signal?: AbortSignal): Promise<BenchCase[]>;
  /** The project's scorecards, newest first. */
  scorecards(projectId: string, signal?: AbortSignal): Promise<Scorecard[]>;
  /** Starts a bench run on the default setup; answers it, running. */
  startRun(projectId: string): Promise<BenchRun>;
}

export interface ScoreFeed {
  /** Follows the project's bench run changes; returns what stops it. A status change ("paused", "resynced") says what is on screen may be stale. */
  follow(projectId: string, handlers: { onRun(change: RunChange): void; onStatus?(status: "connecting" | "live" | "paused" | "resynced"): void }): () => void;
}
