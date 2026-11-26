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

export class SettingsViewDto {
  constructor(
    readonly context_map_enabled: BoolFieldDto,
    readonly task_memory_require_update: BoolFieldDto,
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
      BoolFieldDto.fromJSON(trajectory["default"], "effective.score.trajectory_recording.default"),
      missing,
      object(NAME, from["values"] ?? {}, "values"),
    );
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
