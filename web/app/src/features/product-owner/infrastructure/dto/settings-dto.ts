// A project's product owner settings (Contract: product owner, Product owner settings).
// The agent, prompt and digest time are for later pieces: they are not read here.

import { boolean, object, string, type Wire } from "../../../../shared/infrastructure/dto/fields";

const SETTINGS = "SettingsDto";
const UPDATE = "UpdateSettingsRequestDto";

export class SettingsDto {
  constructor(
    readonly project_id: string,
    readonly enabled: boolean,
  ) {}

  static fromJSON(raw: unknown, at = ""): SettingsDto {
    const from = object(SETTINGS, raw, at);
    return new SettingsDto(string(SETTINGS, from, "project_id", at), boolean(SETTINGS, from, "enabled", at));
  }

  toJSON(): Wire {
    return { project_id: this.project_id, enabled: this.enabled };
  }
}

/** The body of POST /api/update_product_owner: only the fields sent change. */
export class UpdateSettingsRequestDto {
  constructor(
    readonly project_id: string,
    readonly enabled: boolean,
  ) {}

  static fromJSON(raw: unknown): UpdateSettingsRequestDto {
    const from = object(UPDATE, raw);
    return new UpdateSettingsRequestDto(string(UPDATE, from, "project_id"), boolean(UPDATE, from, "enabled"));
  }

  toJSON(): Wire {
    return { project_id: this.project_id, enabled: this.enabled };
  }
}
