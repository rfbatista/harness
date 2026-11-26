// The Agent system island's props: the project and the repository the address names ("" for the project defaults).

import { object, optionalString, string, type Wire } from "../../../../shared/infrastructure/dto/fields";

const NAME = "SettingsPropsDto";

export class SettingsPropsDto {
  constructor(
    readonly project_id: string,
    readonly project_name: string,
    readonly repository: string,
  ) {}

  static fromJSON(raw: unknown): SettingsPropsDto {
    const from = object(NAME, raw);
    return new SettingsPropsDto(string(NAME, from, "project_id"), string(NAME, from, "project_name"), optionalString(NAME, from, "repository") ?? "");
  }

  toJSON(): Wire {
    return { project_id: this.project_id, project_name: this.project_name, repository: this.repository };
  }
}
