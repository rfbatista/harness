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
    /** The agent of the session that started this one; "" when a person did. */
    readonly parent: string,
    /** The person asked for the tab by key: its search box takes the focus. */
    readonly focus: boolean,
    /** Where the task page shows the agents whiteboard with this session selected; "" off a task page. */
    readonly whiteboard_href: string,
  ) {}

  static fromJSON(raw: unknown): ContextPropsDto {
    const from = object(NAME, raw);
    return new ContextPropsDto(string(NAME, from, "session_id"), string(NAME, from, "project_id"), boolean(NAME, from, "live"), optionalString(NAME, from, "group") ?? "", optionalString(NAME, from, "parent") ?? "", from.focus === true, optionalString(NAME, from, "whiteboard_href") ?? "");
  }

  toJSON(): Wire {
    return { session_id: this.session_id, project_id: this.project_id, live: this.live, group: this.group, parent: this.parent, focus: this.focus, whiteboard_href: this.whiteboard_href };
  }
}
