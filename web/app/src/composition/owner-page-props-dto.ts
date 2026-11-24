// The island props of the product owner's page (the Go page handler): its project, and the thread its address names.

import { object, string, type Wire } from "../shared/infrastructure/dto/fields";

const NAME = "OwnerPagePropsDto";

export class OwnerPagePropsDto {
  constructor(
    readonly project_id: string,
    readonly project_name: string,
    readonly thread_id: string,
  ) {}

  static fromJSON(raw: unknown): OwnerPagePropsDto {
    const from = object(NAME, raw);
    return new OwnerPagePropsDto(string(NAME, from, "project_id"), string(NAME, from, "project_name"), string(NAME, from, "thread_id"));
  }

  toJSON(): Wire {
    return { project_id: this.project_id, project_name: this.project_name, thread_id: this.thread_id };
  }
}
