// A project's bench cases and runs, as the Score page reads them.

export interface BenchCase {
  readonly id: string;
  readonly name: string;
  readonly prompt: string;
  /** The names of the mock tools the case offers. */
  readonly tools: readonly string[];
  readonly source: "definition" | "trajectory";
}

export type RunStatus = "running" | "finished" | "cancelled" | "failed";

export interface BenchRun {
  readonly id: string;
  readonly status: RunStatus;
  readonly total: number;
  readonly done: number;
  readonly scorecardId: string;
  readonly error: string;
}

/** A run's progress or end, as the project feed announces it. */
export interface RunChange {
  readonly runId: string;
  readonly status: RunStatus;
  readonly done: number;
  readonly total: number;
  readonly scorecardId: string;
}

export type ScoreErrorCode = "UNAVAILABLE" | "NOT_FOUND" | "NETWORK_ERROR" | "BAD_RESPONSE" | "UNKNOWN";

/** A failed read or call: the code says what the page can do about it. */
export class ScoreError extends Error {
  readonly code: ScoreErrorCode;
  constructor(code: ScoreErrorCode, message: string) {
    super(message);
    this.name = "ScoreError";
    this.code = code;
  }
}
