// The Score island's props: the project.

import { object, string, type Wire } from "../../../../shared/infrastructure/dto/fields";

const NAME = "ScorePropsDto";

export class ScorePropsDto {
  constructor(
    readonly project_id: string,
    readonly project_name: string,
  ) {}

  static fromJSON(raw: unknown): ScorePropsDto {
    const from = object(NAME, raw);
    return new ScorePropsDto(string(NAME, from, "project_id"), string(NAME, from, "project_name"));
  }

  toJSON(): Wire {
    return { project_id: this.project_id, project_name: this.project_name };
  }
}
