// The detail of the window's "harness:navigated" event (Contract: Harness server
// ↔ Web UI — the top bar island, §4): the new page's path.

import { object, string, type Wire } from "../../../../shared/infrastructure/dto/fields";

const NAME = "NavigatedDto";

export class NavigatedDto {
  constructor(readonly path: string) {}

  static fromJSON(raw: unknown): NavigatedDto {
    return new NavigatedDto(string(NAME, object(NAME, raw), "path"));
  }

  toJSON(): Wire {
    return { path: this.path };
  }
}
