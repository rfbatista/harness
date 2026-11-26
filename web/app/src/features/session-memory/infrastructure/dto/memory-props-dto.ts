// The memory island's props: the session it shows and whether it is running, written into the page by the session head.

import { boolean, object, string, type Wire } from "../../../../shared/infrastructure/dto/fields";

const NAME = "MemoryPropsDto";

export class MemoryPropsDto {
  constructor(
    readonly session_id: string,
    readonly live: boolean,
  ) {}

  static fromJSON(raw: unknown): MemoryPropsDto {
    const from = object(NAME, raw);
    return new MemoryPropsDto(string(NAME, from, "session_id"), boolean(NAME, from, "live"));
  }

  toJSON(): Wire {
    return { session_id: this.session_id, live: this.live };
  }
}
