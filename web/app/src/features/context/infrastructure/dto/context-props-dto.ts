// The Context tab island's props: the session it shows and its project (whose
// feed announces changes), written into the page by the session pane.

import { object, string, type Wire } from "../../../../shared/infrastructure/dto/fields";

const NAME = "ContextPropsDto";

export class ContextPropsDto {
  constructor(
    readonly session_id: string,
    readonly project_id: string,
  ) {}

  static fromJSON(raw: unknown): ContextPropsDto {
    const from = object(NAME, raw);
    return new ContextPropsDto(string(NAME, from, "session_id"), string(NAME, from, "project_id"));
  }

  toJSON(): Wire {
    return { session_id: this.session_id, project_id: this.project_id };
  }
}
