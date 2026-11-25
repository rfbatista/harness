// The rail seed the server paints the first paint from (shell.RailSeed,
// script#rail-seed) and the island props carry; and the answers of the two
// lists read again after a drop. Fields keep the wire names.

import { array, lenientArray, object, optionalCount, string, type Wire } from "../../../../shared/infrastructure/dto/fields";
import { RailSessionDto } from "./session-dto";
import { TaskDto } from "./task-dto";

const SEED = "RailSeedDto";

export class RailSeedDto {
  constructor(
    readonly project_id: string,
    readonly sessions: readonly RailSessionDto[],
    readonly tasks: readonly TaskDto[],
    readonly archived_count: number,
  ) {}

  static fromJSON(raw: unknown): RailSeedDto {
    const from = object(SEED, raw);
    return new RailSeedDto(
      string(SEED, from, "project_id"),
      // One the client cannot read (a status it does not know) is left out, so the rail works for the rest.
      lenientArray(SEED, from, "sessions", (item, at) => RailSessionDto.fromJSON(item, at)),
      lenientArray(SEED, from, "tasks", (item, at) => TaskDto.fromJSON(item, at)),
      optionalCount(SEED, from, "archived_count") ?? 0,
    );
  }

  toJSON(): Wire {
    return { project_id: this.project_id, sessions: this.sessions.map((s) => s.toJSON()), tasks: this.tasks.map((t) => t.toJSON()), archived_count: this.archived_count };
  }
}

/** GET /api/sessions?project_id= : { sessions: [...] }. */
export class SessionListDto {
  constructor(readonly sessions: readonly RailSessionDto[]) {}

  static fromJSON(raw: unknown): SessionListDto {
    const from = object("SessionListDto", raw);
    array("SessionListDto", from, "sessions", () => undefined);
    return new SessionListDto(lenientArray("SessionListDto", from, "sessions", (item, at) => RailSessionDto.fromJSON(item, at)));
  }

  toJSON(): Wire {
    return { sessions: this.sessions.map((s) => s.toJSON()) };
  }
}

/** GET /api/list_tickets?project_id= : { tickets: [...] }. The list must be there; a ticket that does not read is left out. */
export class TaskListDto {
  constructor(readonly tickets: readonly TaskDto[]) {}

  static fromJSON(raw: unknown): TaskListDto {
    const from = object("TaskListDto", raw);
    array("TaskListDto", from, "tickets", () => undefined);
    return new TaskListDto(lenientArray("TaskListDto", from, "tickets", (item, at) => TaskDto.fromJSON(item, at)));
  }

  toJSON(): Wire {
    return { tickets: this.tickets.map((t) => t.toJSON()) };
  }
}
