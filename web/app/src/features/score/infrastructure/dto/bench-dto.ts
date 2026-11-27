// The bench's cases (GET .../bench/cases), a run (POST .../bench/runs) and a run change on the feed.

import { array, count, lenientArray, object, oneOf, optionalString, string, time } from "../../../../shared/infrastructure/dto/fields";

const STATUSES = ["running", "finished", "cancelled", "failed"] as const;

export class MockToolDto {
  constructor(
    readonly name: string,
    readonly description: string,
    readonly result: string,
    readonly is_error: boolean,
  ) {}

  static fromJSON(raw: unknown, at: string): MockToolDto {
    const from = object("BenchCaseDto", raw, at);
    return new MockToolDto(string("BenchCaseDto", from, "name", at), optionalString("BenchCaseDto", from, "description", at) ?? "", optionalString("BenchCaseDto", from, "result", at) ?? "", from["is_error"] === true);
  }
}

export class ExpectedCallDto {
  constructor(
    readonly tool: string,
    readonly params: Readonly<Record<string, unknown>> | null,
  ) {}

  static fromJSON(raw: unknown, at: string): ExpectedCallDto {
    const from = object("BenchCaseDto", raw, at);
    const params = from["params"];
    return new ExpectedCallDto(string("BenchCaseDto", from, "tool", at), params === undefined || params === null ? null : object("BenchCaseDto", params, `${at}.params`));
  }
}

export class ExpectationDto {
  constructor(
    readonly tools: readonly ExpectedCallDto[],
    readonly ordered: boolean,
    readonly reference: string,
  ) {}

  static fromJSON(raw: unknown, at: string): ExpectationDto {
    const from = raw === undefined || raw === null ? {} : object("BenchCaseDto", raw, at);
    const tools = from["tools"] === undefined || from["tools"] === null ? [] : array("BenchCaseDto", from, "tools", (t, path) => ExpectedCallDto.fromJSON(t, path), at);
    return new ExpectationDto(tools, from["ordered"] === true, optionalString("BenchCaseDto", from, "reference", at) ?? "");
  }
}

export class BenchCaseDto {
  constructor(
    readonly id: string,
    readonly name: string,
    readonly prompt: string,
    readonly tools: readonly MockToolDto[],
    readonly expect: ExpectationDto,
    readonly source: "definition" | "trajectory",
  ) {}

  static fromJSON(raw: unknown, at: string): BenchCaseDto {
    const from = object("BenchCaseDto", raw, at);
    const tools = from["tools"] === undefined || from["tools"] === null ? [] : array("BenchCaseDto", from, "tools", (t, path) => MockToolDto.fromJSON(t, path), at);
    return new BenchCaseDto(string("BenchCaseDto", from, "id", at), optionalString("BenchCaseDto", from, "name", at) ?? "", optionalString("BenchCaseDto", from, "prompt", at) ?? "", tools, ExpectationDto.fromJSON(from["expect"], `${at}.expect`), from["source"] === "trajectory" ? "trajectory" : "definition");
  }
}

/** A refused definition or expectation: 422 `{ errors: [{ field, code, message }] }`. */
export class FieldErrorsDto {
  constructor(readonly errors: readonly { field: string; code: string; message: string }[]) {}

  static fromJSON(raw: unknown): FieldErrorsDto {
    const from = object("FieldErrorsDto", raw);
    return new FieldErrorsDto(array("FieldErrorsDto", from, "errors", (item, at) => {
      const e = object("FieldErrorsDto", item, at);
      return { field: string("FieldErrorsDto", e, "field", at), code: string("FieldErrorsDto", e, "code", at), message: string("FieldErrorsDto", e, "message", at) };
    }));
  }
}

export class TrajectoryDto {
  constructor(
    readonly id: string,
    readonly task_id: string,
    readonly prompt: string,
    readonly steps: number,
    readonly recorded_at: Date,
  ) {}

  static fromJSON(raw: unknown, at: string): TrajectoryDto {
    const from = object("TrajectoryDto", raw, at);
    const steps = from["steps"] === undefined || from["steps"] === null ? [] : array("TrajectoryDto", from, "steps", (s) => s, at);
    return new TrajectoryDto(string("TrajectoryDto", from, "id", at), optionalString("TrajectoryDto", from, "task_id", at) ?? "", optionalString("TrajectoryDto", from, "prompt", at) ?? "", steps.length, time("TrajectoryDto", from, "recorded_at", at));
  }
}

export class TrajectoriesDto {
  constructor(readonly trajectories: readonly TrajectoryDto[]) {}

  static fromJSON(raw: unknown): TrajectoriesDto {
    const from = object("TrajectoriesDto", raw);
    return new TrajectoriesDto(lenientArray("TrajectoriesDto", from, "trajectories", (item, at) => TrajectoryDto.fromJSON(item, at)));
  }
}

export class BenchCasesDto {
  constructor(readonly cases: readonly BenchCaseDto[]) {}

  static fromJSON(raw: unknown): BenchCasesDto {
    const from = object("BenchCasesDto", raw);
    return new BenchCasesDto(lenientArray("BenchCasesDto", from, "cases", (item, at) => BenchCaseDto.fromJSON(item, at)));
  }
}

export class BenchRunDto {
  constructor(
    readonly id: string,
    readonly status: (typeof STATUSES)[number],
    readonly total: number,
    readonly done: number,
    readonly scorecard_id: string,
    readonly error: string,
    readonly started_at: Date | null,
  ) {}

  static fromJSON(raw: unknown): BenchRunDto {
    const from = object("BenchRunDto", raw);
    return new BenchRunDto(string("BenchRunDto", from, "id"), oneOf("BenchRunDto", from, "status", STATUSES), from["total"] === undefined ? 0 : count("BenchRunDto", from, "total"), from["done"] === undefined ? 0 : count("BenchRunDto", from, "done"), optionalString("BenchRunDto", from, "scorecard_id") ?? "", optionalString("BenchRunDto", from, "error") ?? "", from["started_at"] === undefined ? null : time("BenchRunDto", from, "started_at"));
  }
}

/** A feed message `{ bench_run: { run_id, status, done, total, scorecard_id? } }`. */
export class RunChangeDto {
  constructor(
    readonly run_id: string,
    readonly status: (typeof STATUSES)[number],
    readonly done: number,
    readonly total: number,
    readonly scorecard_id: string,
  ) {}

  static fromJSON(raw: unknown): RunChangeDto {
    const outer = object("RunChangeDto", raw);
    const from = object("RunChangeDto", outer["bench_run"], "bench_run");
    return new RunChangeDto(string("RunChangeDto", from, "run_id", "bench_run"), oneOf("RunChangeDto", from, "status", STATUSES, "bench_run"), from["done"] === undefined ? 0 : count("RunChangeDto", from, "done", "bench_run"), from["total"] === undefined ? 0 : count("RunChangeDto", from, "total", "bench_run"), optionalString("RunChangeDto", from, "scorecard_id", "bench_run") ?? "");
  }
}

/** What is sent for an expectation (PATCH .../bench/cases/{case}): `{ expect }`. */
export class ExpectationRequestDto {
  constructor(readonly expect: { tools: readonly { tool: string; params: Readonly<Record<string, unknown>> | null }[]; ordered: boolean; reference: string }) {}

  toJSON(): unknown {
    return { expect: { tools: this.expect.tools.map((t) => (t.params === null ? { tool: t.tool } : { tool: t.tool, params: t.params })), ordered: this.expect.ordered, reference: this.expect.reference } };
  }
}

/** What is sent for a new case (POST .../bench/cases). */
export class CaseDefinitionRequestDto {
  constructor(
    readonly name: string,
    readonly prompt: string,
    readonly tools: readonly { name: string; description: string; result: string; is_error: boolean }[],
    readonly expect: ExpectationRequestDto,
  ) {}

  toJSON(): unknown {
    return { name: this.name, prompt: this.prompt, tools: this.tools.map((t) => ({ name: t.name, ...(t.description !== "" ? { description: t.description } : {}), result: t.result, ...(t.is_error ? { is_error: true } : {}) })), expect: (this.expect.toJSON() as { expect: unknown }).expect };
  }
}

/** What is sent to make a case from a recorded trajectory. */
export class TrajectoryRequestDto {
  constructor(readonly trajectory_id: string) {}

  toJSON(): unknown {
    return { trajectory_id: this.trajectory_id };
  }
}

/** What is sent to start a run of some of the cases. */
export class RunRequestDto {
  constructor(readonly case_ids: readonly string[]) {}

  toJSON(): unknown {
    return { case_ids: this.case_ids };
  }
}
