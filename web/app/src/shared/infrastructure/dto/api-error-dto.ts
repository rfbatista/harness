// The API's error body: { error: message, code }.

import { object, optionalString, string, type Wire } from "./fields";

const NAME = "ApiErrorDto";

export class ApiErrorDto {
  constructor(
    readonly error: string,
    readonly code: string,
    /** Some refusals say where the person fixes it (PRODUCT_OWNER_UNAVAILABLE). */
    readonly fixUrl?: string,
  ) {}

  static fromJSON(raw: unknown): ApiErrorDto {
    const from = object(NAME, raw);
    return new ApiErrorDto(string(NAME, from, "error"), string(NAME, from, "code"), optionalString(NAME, from, "fix_url"));
  }

  toJSON(): Wire {
    return { error: this.error, code: this.code, ...(this.fixUrl === undefined ? {} : { fix_url: this.fixUrl }) };
  }
}
