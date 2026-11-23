// The props of the agents whiteboard island: the task the page shows and what
// the page already knows about it (the agent names and the task's title).

import { DtoError } from "../shared/infrastructure/dto/dto-error";
import { object, optionalString, string, type Wire } from "../shared/infrastructure/dto/fields";

const DTO = "WhiteboardPropsDto";

export class WhiteboardPropsDto {
  constructor(
    readonly project_id: string,
    readonly ticket_id: string,
    readonly task_title: string,
    readonly agent_names: Record<string, string>,
    /** A session to open selected, from `view=whiteboard&session=<id>`. */
    readonly session: string,
  ) {}

  static fromJSON(raw: unknown): WhiteboardPropsDto {
    const from = object(DTO, raw);
    const names: Record<string, string> = {};
    const given = from.agent_names;
    if (given !== undefined && given !== null) {
      if (typeof given !== "object" || Array.isArray(given)) throw new DtoError(DTO, "agent_names", "expected an object");
      for (const [id, name] of Object.entries(given)) if (typeof name === "string") names[id] = name;
    }
    return new WhiteboardPropsDto(string(DTO, from, "project_id"), string(DTO, from, "ticket_id"), optionalString(DTO, from, "task_title") ?? "", names, optionalString(DTO, from, "session") ?? "");
  }

  toJSON(): Wire {
    return { project_id: this.project_id, ticket_id: this.ticket_id, task_title: this.task_title, agent_names: this.agent_names, session: this.session };
  }
}
