// The body of POST /api/notifications/read: the ids to mark, or `all`.

import { DtoError } from "../../../../shared/infrastructure/dto/dto-error";
import { object, type Wire } from "../../../../shared/infrastructure/dto/fields";

const NAME = "ReadRequestDto";

export class ReadRequestDto {
  constructor(
    readonly ids?: readonly string[],
    readonly all?: true,
  ) {}

  static fromJSON(raw: unknown): ReadRequestDto {
    const from = object(NAME, raw);
    const { ids, all } = from;
    if (ids !== undefined && (!Array.isArray(ids) || ids.some((id) => typeof id !== "string"))) throw new DtoError(NAME, "ids", "expected an array of strings");
    if (all !== undefined && all !== true) throw new DtoError(NAME, "all", "expected true");
    return new ReadRequestDto(ids as string[] | undefined, all);
  }

  toJSON(): Wire {
    return { ...(this.ids === undefined ? {} : { ids: this.ids }), ...(this.all === undefined ? {} : { all: this.all }) };
  }
}
