// The island props of the product owner's panel: the project it belongs to (Frame.OwnerPanelProps).

import { boolean, object, string, type Wire } from "../shared/infrastructure/dto/fields";

const NAME = "OwnerPanelPropsDto";

export class OwnerPanelPropsDto {
  constructor(
    readonly project_id: string,
    readonly project_name: string,
    readonly no_tasks: boolean,
  ) {}

  static fromJSON(raw: unknown): OwnerPanelPropsDto {
    const from = object(NAME, raw);
    return new OwnerPanelPropsDto(string(NAME, from, "project_id"), string(NAME, from, "project_name"), boolean(NAME, from, "no_tasks"));
  }

  toJSON(): Wire {
    return { project_id: this.project_id, project_name: this.project_name, no_tasks: this.no_tasks };
  }
}
