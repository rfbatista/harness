// GET /api/projects/{p}/agent-system and the repository's: { effective, values }. Only the fields the
// page shows are read; unknown ones are never looked at.

import { array, object, oneOf, optionalString, string, type Wire } from "../../../../shared/infrastructure/dto/fields";
import { DtoError } from "../../../../shared/infrastructure/dto/dto-error";

const NAME = "SettingsViewDto";
const SOURCES = ["repository", "project", "harness"] as const;

export class BoolFieldDto {
  constructor(
    readonly value: boolean,
    readonly source: (typeof SOURCES)[number],
    readonly reason: string,
  ) {}

  static fromJSON(raw: unknown, at: string): BoolFieldDto {
    const from = object(NAME, raw, at);
    if (typeof from["value"] !== "boolean") throw new DtoError(NAME, `${at}.value`, "expected a boolean");
    return new BoolFieldDto(from["value"], oneOf(NAME, from, "source", SOURCES, at), optionalString(NAME, from, "reason", at) ?? "");
  }
}

export class NumberFieldDto {
  constructor(
    readonly value: number,
    readonly source: (typeof SOURCES)[number],
    readonly reason: string,
  ) {}

  static fromJSON(raw: unknown, at: string): NumberFieldDto {
    const from = object(NAME, raw, at);
    if (typeof from["value"] !== "number") throw new DtoError(NAME, `${at}.value`, "expected a number");
    return new NumberFieldDto(from["value"], oneOf(NAME, from, "source", SOURCES, at), optionalString(NAME, from, "reason", at) ?? "");
  }
}

export class TextFieldDto {
  constructor(
    readonly value: string,
    readonly source: (typeof SOURCES)[number],
    readonly reason: string,
  ) {}

  static fromJSON(raw: unknown, at: string): TextFieldDto {
    const from = object(NAME, raw, at);
    if (typeof from["value"] !== "string") throw new DtoError(NAME, `${at}.value`, "expected text");
    return new TextFieldDto(from["value"], oneOf(NAME, from, "source", SOURCES, at), optionalString(NAME, from, "reason", at) ?? "");
  }
}

export class ListFieldDto {
  constructor(
    readonly value: readonly string[],
    readonly source: (typeof SOURCES)[number],
    readonly reason: string,
  ) {}

  static fromJSON(raw: unknown, at: string): ListFieldDto {
    const from = object(NAME, raw, at);
    const value = from["value"] === null ? [] : from["value"];
    if (!Array.isArray(value) || !value.every((v) => typeof v === "string")) throw new DtoError(NAME, `${at}.value`, "expected a list of text");
    return new ListFieldDto(value as string[], oneOf(NAME, from, "source", SOURCES, at), optionalString(NAME, from, "reason", at) ?? "");
  }
}

export class GateDto {
  constructor(
    readonly kind: string,
    readonly command: TextFieldDto,
    readonly timeout_seconds: NumberFieldDto,
  ) {}

  static fromJSON(raw: unknown, at: string): GateDto {
    const from = object(NAME, raw, at);
    return new GateDto(string(NAME, from, "kind", at), TextFieldDto.fromJSON(from["command"], `${at}.command`), NumberFieldDto.fromJSON(from["timeout_seconds"], `${at}.timeout_seconds`));
  }
}

export class GuardDto {
  constructor(
    readonly gates: readonly GateDto[],
    readonly protected_paths: ListFieldDto,
    readonly lockfile_rule_enabled: BoolFieldDto,
    readonly retry_limit: NumberFieldDto,
    readonly tests_fail_first_available: BoolFieldDto,
  ) {}

  static fromJSON(raw: unknown): GuardDto {
    const from = object(NAME, raw, "effective.guard");
    const gates = from["gates"] === null || from["gates"] === undefined ? [] : array(NAME, from, "gates", (item, at) => GateDto.fromJSON(item, at), "effective.guard");
    const lockfile = object(NAME, from["lockfile_rule"], "effective.guard.lockfile_rule");
    const testsFirst = object(NAME, from["tests_fail_first"], "effective.guard.tests_fail_first");
    return new GuardDto(
      gates,
      ListFieldDto.fromJSON(from["protected_paths"], "effective.guard.protected_paths"),
      BoolFieldDto.fromJSON(lockfile["enabled"], "effective.guard.lockfile_rule.enabled"),
      NumberFieldDto.fromJSON(from["retry_limit"], "effective.guard.retry_limit"),
      BoolFieldDto.fromJSON(testsFirst["available"], "effective.guard.tests_fail_first.available"),
    );
  }
}

/** The UI declaration as the store keeps it; absent where no level declares one. */
export class VisualQaFieldDto {
  constructor(
    readonly value: { readonly start_command: string; readonly url: string; readonly routes: readonly string[]; readonly viewports: readonly string[]; readonly ui_paths: readonly string[] } | null,
    readonly source: (typeof SOURCES)[number],
    readonly reason: string,
  ) {}

  static fromJSON(raw: unknown, at: string): VisualQaFieldDto {
    const from = object(NAME, raw, at);
    const source = oneOf(NAME, from, "source", SOURCES, at);
    if (from["value"] === null || from["value"] === undefined) return new VisualQaFieldDto(null, source, "");
    const ui = object(NAME, from["value"], `${at}.value`);
    const strings = (key: string): string[] => (ui[key] === null || ui[key] === undefined ? [] : array(NAME, ui, key, (item, here) => {
      if (typeof item !== "string") throw new DtoError(NAME, here, "expected text");
      return item;
    }, `${at}.value`));
    return new VisualQaFieldDto({ start_command: optionalString(NAME, ui, "start_command", `${at}.value`) ?? "", url: optionalString(NAME, ui, "url", `${at}.value`) ?? "", routes: strings("routes"), viewports: strings("viewports"), ui_paths: strings("ui_paths") }, source, "");
  }
}

export class CriticDto {
  constructor(
    readonly enabled: BoolFieldDto,
    readonly model: TextFieldDto,
    readonly standards: ListFieldDto,
    readonly visual_qa_ui: VisualQaFieldDto,
  ) {}

  /** An answer without a review block reads as the harness defaults: the store always sends one. */
  static fromJSON(raw: unknown): CriticDto {
    if (raw === undefined || raw === null) {
      const harness = { source: "harness" };
      return CriticDto.fromJSON({ critic: { enabled: { value: true, ...harness }, model: { value: "", ...harness }, standards: { value: [], ...harness } }, visual_qa: { ui: { value: null, ...harness } } });
    }
    const review = object(NAME, raw, "effective.review");
    const critic = object(NAME, review["critic"], "effective.review.critic");
    const visual = object(NAME, review["visual_qa"], "effective.review.visual_qa");
    return new CriticDto(
      BoolFieldDto.fromJSON(critic["enabled"], "effective.review.critic.enabled"),
      TextFieldDto.fromJSON(critic["model"], "effective.review.critic.model"),
      ListFieldDto.fromJSON(critic["standards"], "effective.review.critic.standards"),
      VisualQaFieldDto.fromJSON(visual["ui"], "effective.review.visual_qa.ui"),
    );
  }
}

export class SettingsViewDto {
  constructor(
    readonly context_map_enabled: BoolFieldDto,
    readonly task_memory_require_update: BoolFieldDto,
    readonly guard: GuardDto,
    readonly review: CriticDto,
    readonly trajectory_recording_default: BoolFieldDto,
    readonly missing_required_gates: readonly string[],
    readonly values: Readonly<Record<string, unknown>>,
  ) {}

  static fromJSON(raw: unknown): SettingsViewDto {
    const from = object(NAME, raw);
    const effective = object(NAME, from["effective"], "effective");
    const learn = object(NAME, effective["learn"], "effective.learn");
    const contextMap = object(NAME, learn["context_map"], "effective.learn.context_map");
    const taskMemory = object(NAME, learn["task_memory"], "effective.learn.task_memory");
    const score = object(NAME, effective["score"], "effective.score");
    const trajectory = object(NAME, score["trajectory_recording"], "effective.score.trajectory_recording");
    const missing = effective["missing_required_gates"] === undefined || effective["missing_required_gates"] === null ? [] : array(NAME, effective, "missing_required_gates", (item, at) => {
      if (typeof item !== "string") throw new DtoError(NAME, at, "expected a string");
      return item;
    }, "effective");
    return new SettingsViewDto(
      BoolFieldDto.fromJSON(contextMap["enabled"], "effective.learn.context_map.enabled"),
      BoolFieldDto.fromJSON(taskMemory["require_update"], "effective.learn.task_memory.require_update"),
      GuardDto.fromJSON(effective["guard"]),
      CriticDto.fromJSON(effective["review"]),
      BoolFieldDto.fromJSON(trajectory["default"], "effective.score.trajectory_recording.default"),
      missing,
      object(NAME, from["values"] ?? {}, "values"),
    );
  }
}

/** GET /api/list_repository_run_commands: the commands a repository can run. */
export class RunCommandsDto {
  constructor(readonly run_commands: readonly { name: string; command: string }[]) {}

  static fromJSON(raw: unknown): RunCommandsDto {
    const from = object("RunCommandsDto", raw);
    const list = from["run_commands"] === null || from["run_commands"] === undefined ? [] : array("RunCommandsDto", from, "run_commands", (item, at) => {
      const c = object("RunCommandsDto", item, at);
      return { name: string("RunCommandsDto", c, "name", at), command: string("RunCommandsDto", c, "command", at) };
    });
    return new RunCommandsDto(list);
  }
}

/** GET .../repositories/{r}/agent-system/check-sh-import: { gates: [{ kind, command, reason }] }. */
export class CheckShImportDto {
  constructor(readonly gates: readonly { kind: string; command: string; reason: string }[]) {}

  static fromJSON(raw: unknown): CheckShImportDto {
    const from = object("CheckShImportDto", raw);
    const list = from["gates"] === null || from["gates"] === undefined ? [] : array("CheckShImportDto", from, "gates", (item, at) => {
      const g = object("CheckShImportDto", item, at);
      return { kind: string("CheckShImportDto", g, "kind", at), command: string("CheckShImportDto", g, "command", at), reason: optionalString("CheckShImportDto", g, "reason", at) ?? "" };
    });
    return new CheckShImportDto(list);
  }
}

/** GET /api/projects/{p}/bench/scorecards: newest first; only what the Score line shows is read. */
export class ScorecardsDto {
  constructor(readonly scorecards: readonly { status: string; mean: number; cost_usd: number; cases: number }[]) {}

  static fromJSON(raw: unknown): ScorecardsDto {
    const from = object("ScorecardsDto", raw);
    const list = from["scorecards"] === null || from["scorecards"] === undefined ? [] : array("ScorecardsDto", from, "scorecards", (item, at) => {
      const card = object("ScorecardsDto", item, at);
      const aggregate = object("ScorecardsDto", card["aggregate"] ?? {}, `${at}.aggregate`);
      return {
        status: string("ScorecardsDto", card, "status", at),
        mean: typeof aggregate["mean"] === "number" ? aggregate["mean"] : 0,
        cost_usd: typeof card["cost_usd"] === "number" ? card["cost_usd"] : 0,
        cases: Array.isArray(card["cases"]) ? card["cases"].length : 0,
      };
    });
    return new ScorecardsDto(list);
  }
}

export class RepositoriesDto {
  constructor(readonly repositories: readonly { id: string; name: string }[]) {}

  static fromJSON(raw: unknown): RepositoriesDto {
    const from = object("RepositoriesDto", raw);
    return new RepositoriesDto(array("RepositoriesDto", from, "repositories", (item, at) => {
      const repo = object("RepositoriesDto", item, at);
      return { id: string("RepositoriesDto", repo, "id", at), name: string("RepositoriesDto", repo, "name", at) };
    }));
  }
}

/** The body of a save: field path → value, null to reset. */
export class PatchDto {
  constructor(readonly fields: Readonly<Record<string, unknown>>) {}
  toJSON(): Wire {
    return { ...this.fields };
  }
}

/** The 422 body: { errors: [{ field, code, message }] }. */
export class RefusalDto {
  constructor(readonly errors: readonly { field: string; code: string; message: string }[]) {}

  static fromJSON(raw: unknown): RefusalDto {
    const from = object("RefusalDto", raw);
    return new RefusalDto(array("RefusalDto", from, "errors", (item, at) => {
      const e = object("RefusalDto", item, at);
      return { field: string("RefusalDto", e, "field", at), code: string("RefusalDto", e, "code", at), message: optionalString("RefusalDto", e, "message", at) ?? "" };
    }));
  }
}
