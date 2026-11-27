// A project's bench cases and runs, as the Score page reads them.

/** A tool a case mocks: a run answers every call to it with `result`. */
export interface MockTool {
  readonly name: string;
  readonly description: string;
  readonly result: string;
  readonly isError: boolean;
}

/** A call a good run makes; null params accept any arguments. */
export interface ExpectedCall {
  readonly tool: string;
  readonly params: Readonly<Record<string, unknown>> | null;
}

/** What a good run does: the tools it calls (in order when `ordered`), and/or a reference for its final result. */
export interface Expectation {
  readonly tools: readonly ExpectedCall[];
  readonly ordered: boolean;
  readonly reference: string;
}

export interface BenchCase {
  readonly id: string;
  readonly name: string;
  readonly prompt: string;
  /** The names of the mock tools the case offers. */
  readonly tools: readonly string[];
  readonly mocks: readonly MockTool[];
  readonly expect: Expectation;
  readonly source: "definition" | "trajectory";
}

/** What a person writes for a new case: its id and source come from the harness. */
export interface CaseDefinition {
  readonly name: string;
  readonly prompt: string;
  readonly mocks: readonly MockTool[];
  readonly expect: Expectation;
}

/** A session of a task that opted in to recording, in the bench's case format; it can become a case. */
export interface Trajectory {
  readonly id: string;
  readonly taskId: string;
  readonly prompt: string;
  readonly steps: number;
  readonly recordedAt: Date;
}

/** One reason the harness refused a definition or an expectation. */
export interface FieldError {
  readonly field: string;
  readonly code: string;
  readonly message: string;
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

export type ScoreErrorCode = "UNAVAILABLE" | "NOT_FOUND" | "REFUSED" | "NETWORK_ERROR" | "BAD_RESPONSE" | "UNKNOWN";

/** A failed read or call: the code says what the page can do about it. */
export class ScoreError extends Error {
  readonly code: ScoreErrorCode;
  /** The fields a refused definition or expectation names (code REFUSED); empty otherwise. */
  readonly fields: readonly FieldError[];
  constructor(code: ScoreErrorCode, message: string, fields: readonly FieldError[] = []) {
    super(message);
    this.name = "ScoreError";
    this.code = code;
    this.fields = fields;
  }
}
