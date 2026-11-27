// GET /api/projects/{p}/bench/scorecards: { scorecards: [...] }. Only the fields the page shows are read.

import { DtoError } from "../../../../shared/infrastructure/dto/dto-error";
import { array, lenientArray, object, oneOf, optionalString, string, time, type Wire } from "../../../../shared/infrastructure/dto/fields";

const NAME = "ScorecardDto";
const STATUSES = ["finished", "cancelled", "partial"] as const;
type PatternKey = "right_tool" | "valid_params" | "right_order" | "outcome" | "trajectory";

function number(from: Wire, key: string, at: string): number {
  const value = from[key];
  if (typeof value !== "number" || !Number.isFinite(value)) throw new DtoError(NAME, `${at}.${key}`, "expected a number");
  return value;
}

/** A number that may be absent (zero): an omitted cost or duration. */
function numberOr0(from: Wire, key: string, at: string): number {
  return from[key] === undefined || from[key] === null ? 0 : number(from, key, at);
}

export class PatternScoreDto {
  constructor(
    readonly scored: boolean,
    readonly score: number,
    readonly reasons: readonly string[] = [],
  ) {}

  static fromJSON(raw: unknown, at: string): PatternScoreDto {
    const from = object(NAME, raw, at);
    const reasons = Array.isArray(from["reasons"]) ? from["reasons"].filter((r): r is string => typeof r === "string") : [];
    return new PatternScoreDto(from["scored"] === true, from["scored"] === true ? number(from, "score", at) : 0, reasons);
  }
}

export class TraceCallDto {
  constructor(
    readonly tool: string,
    readonly params: Readonly<Record<string, unknown>>,
  ) {}

  static fromJSON(raw: unknown, at: string): TraceCallDto {
    const from = object(NAME, raw, at);
    return new TraceCallDto(string(NAME, from, "tool", at), from["params"] === undefined || from["params"] === null ? {} : object(NAME, from["params"], `${at}.params`));
  }
}

export class StepNoteDto {
  constructor(
    readonly step: number,
    readonly ok: boolean,
    readonly note: string,
  ) {}

  static fromJSON(raw: unknown, at: string): StepNoteDto {
    const from = object(NAME, raw, at);
    return new StepNoteDto(numberOr0(from, "step", at), from["ok"] === true, optionalString(NAME, from, "note", at) ?? "");
  }
}

export class AggregateDto {
  constructor(
    readonly scored: number,
    readonly mean: number,
  ) {}

  static fromJSON(raw: unknown, at: string): AggregateDto {
    const from = object(NAME, raw, at);
    return new AggregateDto(numberOr0(from, "scored", at), numberOr0(from, "mean", at));
  }
}

export class CaseScoreDto {
  constructor(
    readonly case_id: string,
    readonly case_name: string,
    readonly scores: Readonly<Record<PatternKey, PatternScoreDto>>,
    readonly error: string,
    readonly cost_usd: number,
    readonly duration_ms: number,
    readonly calls: readonly TraceCallDto[],
    readonly steps: readonly StepNoteDto[],
    readonly final_text: string,
  ) {}

  static fromJSON(raw: unknown, at: string): CaseScoreDto {
    const from = object(NAME, raw, at);
    const toolUse = object(NAME, from["tool_use"] ?? {}, `${at}.tool_use`);
    const read = (source: Wire, key: string, where: string): PatternScoreDto => (source[key] === undefined ? new PatternScoreDto(false, 0) : PatternScoreDto.fromJSON(source[key], `${where}.${key}`));
    return new CaseScoreDto(
      string(NAME, from, "case_id", at),
      optionalString(NAME, from, "case_name", at) ?? "",
      {
        right_tool: read(toolUse, "right_tool", `${at}.tool_use`),
        valid_params: read(toolUse, "valid_params", `${at}.tool_use`),
        right_order: read(toolUse, "right_order", `${at}.tool_use`),
        outcome: read(from, "outcome", at),
        trajectory: read(from, "trajectory", at),
      },
      optionalString(NAME, from, "error", at) ?? "",
      numberOr0(from, "cost_usd", at),
      numberOr0(from, "duration_ms", at),
      Array.isArray(from["calls"]) ? from["calls"].map((c, i) => TraceCallDto.fromJSON(c, `${at}.calls[${i}]`)) : [],
      Array.isArray(from["steps"]) ? from["steps"].map((c, i) => StepNoteDto.fromJSON(c, `${at}.steps[${i}]`)) : [],
      optionalString(NAME, from, "final_text", at) ?? "",
    );
  }
}

export class ScorecardDto {
  constructor(
    readonly id: string,
    readonly status: (typeof STATUSES)[number],
    readonly fingerprint: string,
    readonly agent_name: string,
    readonly model: string,
    readonly judge_model: string,
    readonly cli_version: string,
    readonly cases: readonly CaseScoreDto[],
    readonly patterns: Readonly<Record<string, AggregateDto>>,
    readonly aggregate: AggregateDto,
    readonly cost_usd: number,
    readonly duration_ms: number,
    readonly started_at: Date,
    readonly finished_at: Date,
  ) {}

  static fromJSON(raw: unknown, at = ""): ScorecardDto {
    const from = object(NAME, raw, at);
    const setup = object(NAME, from["setup"], `${at}.setup`.replace(/^\./, ""));
    const patternsRaw = from["patterns"] === undefined || from["patterns"] === null ? {} : object(NAME, from["patterns"], "patterns");
    const patterns: Record<string, AggregateDto> = {};
    for (const key of Object.keys(patternsRaw)) patterns[key] = AggregateDto.fromJSON(patternsRaw[key], `patterns.${key}`);
    return new ScorecardDto(
      string(NAME, from, "id", at),
      oneOf(NAME, from, "status", STATUSES, at),
      optionalString(NAME, setup, "fingerprint", "setup") ?? "",
      optionalString(NAME, setup, "agent_name", "setup") ?? "",
      optionalString(NAME, setup, "model", "setup") ?? "",
      optionalString(NAME, setup, "judge_model", "setup") ?? "",
      optionalString(NAME, setup, "cli_version", "setup") ?? "",
      lenientArray(NAME, from, "cases", (item, path) => CaseScoreDto.fromJSON(item, path), at),
      patterns,
      from["aggregate"] === undefined ? new AggregateDto(0, 0) : AggregateDto.fromJSON(from["aggregate"], "aggregate"),
      numberOr0(from, "cost_usd", at),
      numberOr0(from, "duration_ms", at),
      time(NAME, from, "started_at", at),
      from["finished_at"] === undefined || from["finished_at"] === null || String(from["finished_at"]).startsWith("0001") ? time(NAME, from, "started_at", at) : time(NAME, from, "finished_at", at),
    );
  }
}

export class ScorecardsDto {
  constructor(readonly scorecards: readonly ScorecardDto[]) {}

  static fromJSON(raw: unknown): ScorecardsDto {
    const from = object("ScorecardsDto", raw);
    return new ScorecardsDto(from["scorecards"] === undefined || from["scorecards"] === null ? [] : array("ScorecardsDto", from, "scorecards", (item) => ScorecardDto.fromJSON(item)));
  }
}

/** A score before and after, and the change, as the compare endpoint writes it. */
export class DeltaDto {
  constructor(
    readonly before: number,
    readonly after: number,
    readonly change: number,
  ) {}

  static fromJSON(raw: unknown, at: string): DeltaDto {
    const from = object("ComparisonDto", raw, at);
    return new DeltaDto(number(from, "before", at), number(from, "after", at), number(from, "change", at));
  }
}

export class CaseDeltaDto {
  constructor(
    readonly case_id: string,
    readonly status: string,
    /** Absent when the case's mean did not move. */
    readonly mean: DeltaDto | null,
  ) {}

  static fromJSON(raw: unknown, at: string): CaseDeltaDto {
    const from = object("ComparisonDto", raw, at);
    return new CaseDeltaDto(string("ComparisonDto", from, "case_id", at), string("ComparisonDto", from, "status", at), from["mean"] === undefined || from["mean"] === null ? null : DeltaDto.fromJSON(from["mean"], `${at}.mean`));
  }
}

/** GET /api/projects/{p}/bench/scorecards/{id}/compare[?baseline=]: a scorecard set against its baseline. */
export class ComparisonDto {
  constructor(
    readonly previous_id: string,
    readonly fingerprint_changed: boolean,
    readonly aggregate: DeltaDto,
    readonly groups: Readonly<Record<string, DeltaDto>>,
    readonly cases: readonly CaseDeltaDto[],
  ) {}

  static fromJSON(raw: unknown): ComparisonDto {
    const from = object("ComparisonDto", raw);
    const groupsRaw = from["groups"] === undefined || from["groups"] === null ? {} : object("ComparisonDto", from["groups"], "groups");
    const groups: Record<string, DeltaDto> = {};
    for (const key of Object.keys(groupsRaw)) groups[key] = DeltaDto.fromJSON(groupsRaw[key], `groups.${key}`);
    return new ComparisonDto(
      string("ComparisonDto", from, "previous_id", ""),
      from["fingerprint_changed"] === true,
      DeltaDto.fromJSON(from["aggregate"], "aggregate"),
      groups,
      Array.isArray(from["cases"]) ? from["cases"].map((c, i) => CaseDeltaDto.fromJSON(c, `cases[${i}]`)) : [],
    );
  }
}
