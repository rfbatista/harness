// The island props of the Settings page's Product owner section: its project.

import { object, string, type Wire } from "../shared/infrastructure/dto/fields";

const NAME = "OwnerSettingsPropsDto";

export class OwnerSettingsPropsDto {
  constructor(readonly project_id: string) {}

  static fromJSON(raw: unknown): OwnerSettingsPropsDto {
    return new OwnerSettingsPropsDto(string(NAME, object(NAME, raw), "project_id"));
  }

  toJSON(): Wire {
    return { project_id: this.project_id };
  }
}
