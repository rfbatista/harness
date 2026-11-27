// The Agent system island's props: the project and the repository the address names ("" for the project defaults).

import { array, object, optionalString, string, type Wire } from "../../../../shared/infrastructure/dto/fields";

const NAME = "SettingsPropsDto";

export class SettingsPropsDto {
  constructor(
    readonly project_id: string,
    readonly project_name: string,
    readonly repository: string,
    /** The models the harness can call, in the order the Models page offers them. */
    readonly models: readonly { value: string; label: string }[],
  ) {}

  static fromJSON(raw: unknown): SettingsPropsDto {
    const from = object(NAME, raw);
    const models = from["models"] === undefined || from["models"] === null ? [] : array(NAME, from, "models", (item, at) => {
      const m = object(NAME, item, at);
      return { value: string(NAME, m, "value", at), label: string(NAME, m, "label", at) };
    });
    return new SettingsPropsDto(string(NAME, from, "project_id"), string(NAME, from, "project_name"), optionalString(NAME, from, "repository") ?? "", models);
  }

  toJSON(): Wire {
    return { project_id: this.project_id, project_name: this.project_name, repository: this.repository, models: this.models.map((m) => ({ ...m })) };
  }
}
