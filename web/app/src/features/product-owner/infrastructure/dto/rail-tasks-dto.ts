// The tasks the page's rail seed holds (<script id="rail-seed">): only each one's id and status are read here.

import { object, string, type Wire } from "../../../../shared/infrastructure/dto/fields";
import { lenientArray } from "../../../../shared/infrastructure/dto/fields";

const SEED = "RailTasksDto";
const TASK = "RailTaskDto";

export class RailTaskDto {
  constructor(
    readonly id: string,
    readonly status: string,
  ) {}

  static fromJSON(raw: unknown, at = ""): RailTaskDto {
    const from = object(TASK, raw, at);
    return new RailTaskDto(string(TASK, from, "id", at), string(TASK, from, "status", at));
  }

  toJSON(): Wire {
    return { id: this.id, status: this.status };
  }
}

export class RailTasksDto {
  constructor(readonly tasks: readonly RailTaskDto[]) {}

  /** `text` is the seed script's text; one that does not read has no tasks. */
  static fromText(text: string): RailTasksDto {
    try {
      const from = object(SEED, JSON.parse(text) as unknown);
      return new RailTasksDto(lenientArray(SEED, from, "tasks", (item, at) => RailTaskDto.fromJSON(item, at)));
    } catch {
      return new RailTasksDto([]);
    }
  }

  toJSON(): Wire {
    return { tasks: this.tasks.map((t) => t.toJSON()) };
  }
}

