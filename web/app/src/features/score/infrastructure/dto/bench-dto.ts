// The bench's cases (GET .../bench/cases), a run (POST .../bench/runs) and a run change on the feed.

import { array, count, lenientArray, object, oneOf, optionalString, string } from "../../../../shared/infrastructure/dto/fields";

const STATUSES = ["running", "finished", "cancelled", "failed"] as const;

export class BenchCaseDto {
  constructor(
    readonly id: string,
    readonly name: string,
    readonly prompt: string,
    readonly tools: readonly string[],
    readonly source: "definition" | "trajectory",
  ) {}

  static fromJSON(raw: unknown, at: string): BenchCaseDto {
    const from = object("BenchCaseDto", raw, at);
    const tools = from["tools"] === undefined || from["tools"] === null ? [] : array("BenchCaseDto", from, "tools", (t, path) => string("BenchCaseDto", object("BenchCaseDto", t, path), "name", path), at);
    return new BenchCaseDto(string("BenchCaseDto", from, "id", at), optionalString("BenchCaseDto", from, "name", at) ?? "", optionalString("BenchCaseDto", from, "prompt", at) ?? "", tools, from["source"] === "trajectory" ? "trajectory" : "definition");
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
  ) {}

  static fromJSON(raw: unknown): BenchRunDto {
    const from = object("BenchRunDto", raw);
    return new BenchRunDto(string("BenchRunDto", from, "id"), oneOf("BenchRunDto", from, "status", STATUSES), from["total"] === undefined ? 0 : count("BenchRunDto", from, "total"), from["done"] === undefined ? 0 : count("BenchRunDto", from, "done"), optionalString("BenchRunDto", from, "scorecard_id") ?? "", optionalString("BenchRunDto", from, "error") ?? "");
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
