// What the Score page needs from the harness: the project's cases and scorecards, a run to start, and the feed that follows it.

import type { BenchCase, BenchRun, CaseDefinition, ExpectedCall, Expectation, RunChange, Trajectory } from "../domain/bench";
import type { LineError } from "../domain/case-text";
import type { ScoreComparison } from "../domain/comparison";
import type { Scorecard } from "../domain/scorecard";

export interface ScoreSource {
  /** The project's bench cases. Fails with a ScoreError. */
  cases(projectId: string, signal?: AbortSignal): Promise<BenchCase[]>;
  /** The project's scorecards, newest first. */
  scorecards(projectId: string, signal?: AbortSignal): Promise<Scorecard[]>;
  /** Starts a bench run on the default setup; answers it, running. With `caseIds` it runs only those cases (a replay); its scorecard is partial. */
  startRun(projectId: string, caseIds?: readonly string[]): Promise<BenchRun>;
  /** One run: how far it is, and why it failed. Fails with a ScoreError NOT_FOUND for a run the harness does not know. */
  run(projectId: string, runId: string): Promise<BenchRun>;
  /** Asks the harness to stop a running run (its end follows on the feed); a run that has ended is refused with a ScoreError. */
  cancelRun(projectId: string, runId: string): Promise<BenchRun>;
  /** A scorecard set against `baselineId`, or against the previous finished run when null; null when there is nothing earlier to compare with. */
  compare(projectId: string, scorecardId: string, baselineId: string | null, signal?: AbortSignal): Promise<ScoreComparison | null>;
  /** Stores a case from a definition. A refused definition fails with a ScoreError REFUSED naming its fields. */
  createCase(projectId: string, definition: CaseDefinition): Promise<BenchCase>;
  /** Replaces what a good run of the case does; refused like a definition. */
  updateExpectation(projectId: string, caseId: string, expect: Expectation): Promise<BenchCase>;
  deleteCase(projectId: string, caseId: string): Promise<void>;
  /** The sessions recorded for tasks that opted in, as cases to be. */
  trajectories(projectId: string, signal?: AbortSignal): Promise<Trajectory[]>;
  caseFromTrajectory(projectId: string, trajectoryId: string): Promise<BenchCase>;
}

export interface ScoreFeed {
  /** Follows the project's bench run changes; returns what stops it. A status change ("paused", "resynced") says what is on screen may be stale. */
  follow(projectId: string, handlers: { onRun(change: RunChange): void; onStatus?(status: "connecting" | "live" | "paused" | "resynced"): void }): () => void;
}

/** The text form of the calls a case expects, one per line: `tool`, or `tool {"param": value}` (the parameters are JSON, read and written by the adapter). */
export interface CallsText {
  /** A line whose parameters are not a JSON object is an error on its line. */
  parse(text: string): { calls: ExpectedCall[]; errors: LineError[] };
  format(calls: readonly ExpectedCall[]): string;
  /** One call a run made, as it reads in a trace. */
  formatCall(tool: string, params: Readonly<Record<string, unknown>>): string;
}
