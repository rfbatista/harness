// A project's product owner settings (Contract: product owner, Product owner settings).
// The digest time and the time zone are read; the agent and prompt are for another piece.

import { DtoError } from "../../../../shared/infrastructure/dto/dto-error";
import { boolean, object, string, type Wire } from "../../../../shared/infrastructure/dto/fields";

const SETTINGS = "SettingsDto";
const UPDATE = "UpdateSettingsRequestDto";

export class SettingsDto {
  constructor(
    readonly project_id: string,
    readonly enabled: boolean,
    /** "HH:MM"; null while no digest time is set. */
    readonly digest_time: string | null,
    /** The server's time zone. */
    readonly time_zone: string,
  ) {}

  static fromJSON(raw: unknown, at = ""): SettingsDto {
    const from = object(SETTINGS, raw, at);
    const time = from.digest_time;
    if (time !== undefined && time !== null && typeof time !== "string") throw new DtoError(SETTINGS, at === "" ? "digest_time" : `${at}.digest_time`, "expected a string or null");
    return new SettingsDto(string(SETTINGS, from, "project_id", at), boolean(SETTINGS, from, "enabled", at), time === undefined || time === "" ? null : time, string(SETTINGS, from, "time_zone", at));
  }

  toJSON(): Wire {
    return { project_id: this.project_id, enabled: this.enabled, digest_time: this.digest_time, time_zone: this.time_zone };
  }
}

/** The body of POST /api/update_product_owner: only the fields sent change. */
export class UpdateSettingsRequestDto {
  constructor(
    readonly project_id: string,
    readonly enabled?: boolean,
    /** "" clears the time. */
    readonly digest_time?: string,
  ) {}

  static fromJSON(raw: unknown): UpdateSettingsRequestDto {
    const from = object(UPDATE, raw);
    return new UpdateSettingsRequestDto(
      string(UPDATE, from, "project_id"),
      from.enabled === undefined ? undefined : boolean(UPDATE, from, "enabled"),
      from.digest_time === undefined ? undefined : string(UPDATE, from, "digest_time"),
    );
  }

  toJSON(): Wire {
    return { project_id: this.project_id, ...(this.enabled === undefined ? {} : { enabled: this.enabled }), ...(this.digest_time === undefined ? {} : { digest_time: this.digest_time }) };
  }
}
