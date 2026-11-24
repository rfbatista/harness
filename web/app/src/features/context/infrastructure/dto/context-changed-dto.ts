// The `context.changed` announcement on the project feed (Contract: Harness
// server ↔ Web UI — session context, C5): { kind: "context.changed",
// session_id, reason }. The shared page stream carries the event's data
// without its SSE name, so `kind` is how it is told from the project feed's
// other events.

import { object, oneOf, string, type Wire } from "../../../../shared/infrastructure/dto/fields";

const NAME = "ContextChangedDto";
export const REASONS = ["entry_added", "usage", "mcp_status"] as const;

export class ContextChangedDto {
  readonly kind = "context.changed";

  constructor(
    readonly session_id: string,
    readonly reason: (typeof REASONS)[number],
  ) {}

  static fromJSON(raw: unknown): ContextChangedDto {
    const from = object(NAME, raw);
    oneOf(NAME, from, "kind", ["context.changed"]);
    return new ContextChangedDto(string(NAME, from, "session_id"), oneOf(NAME, from, "reason", REASONS));
  }

  toJSON(): Wire {
    return { kind: this.kind, session_id: this.session_id, reason: this.reason };
  }
}
