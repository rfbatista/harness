// The Context tab island's props: the session it shows and its project (whose
// feed announces changes) and whether it is running, written into the page by the session pane.

import { boolean, object, optionalString, string, type Wire } from "../../../../shared/infrastructure/dto/fields";

const NAME = "ContextPropsDto";

export class ContextPropsDto {
  constructor(
    readonly session_id: string,
    readonly project_id: string,
    readonly live: boolean,
    /** The group a link asked to open ("instructions"), "" for none. */
    readonly group: string,
  ) {}

  static fromJSON(raw: unknown): ContextPropsDto {
    const from = object(NAME, raw);
    return new ContextPropsDto(string(NAME, from, "session_id"), string(NAME, from, "project_id"), boolean(NAME, from, "live"), optionalString(NAME, from, "group") ?? "");
  }

  toJSON(): Wire {
    return { session_id: this.session_id, project_id: this.project_id, live: this.live, group: this.group };
  }
}
