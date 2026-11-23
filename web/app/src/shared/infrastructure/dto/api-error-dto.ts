// The API's error body: { error: message, code }.

import { object, string, type Wire } from "./fields";

const NAME = "ApiErrorDto";

export class ApiErrorDto {
  constructor(
    readonly error: string,
    readonly code: string,
  ) {}

  static fromJSON(raw: unknown): ApiErrorDto {
    const from = object(NAME, raw);
    return new ApiErrorDto(string(NAME, from, "error"), string(NAME, from, "code"));
  }

  toJSON(): Wire {
    return { error: this.error, code: this.code };
  }
}
