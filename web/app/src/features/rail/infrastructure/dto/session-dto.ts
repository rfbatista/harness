// A session as the server sends it (internal/domain/session.go), as much as the
// rail reads: in the seed, in GET /api/sessions and in a `session` change.

import { object, optionalCount, optionalString, string, type Wire } from "../../../../shared/infrastructure/dto/fields";

const NAME = "RailSessionDto";

export class RailSessionDto {
  constructor(
    readonly id: string,
    readonly ticket_id: string,
    readonly status: string,
    readonly pending_approvals: number,
    readonly open_interview_count: number,
  ) {}

  static fromJSON(raw: unknown, at = ""): RailSessionDto {
    const from = object(NAME, raw, at);
    return new RailSessionDto(
      string(NAME, from, "id", at),
      optionalString(NAME, from, "ticket_id", at) ?? "",
      string(NAME, from, "status", at),
      optionalCount(NAME, from, "pending_approvals", at) ?? 0,
      optionalCount(NAME, from, "open_interview_count", at) ?? 0,
    );
  }

  toJSON(): Wire {
    return { id: this.id, ticket_id: this.ticket_id, status: this.status, pending_approvals: this.pending_approvals, open_interview_count: this.open_interview_count };
  }
}
