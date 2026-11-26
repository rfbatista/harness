// The project feed's `ticket` and `session` changes (ports.TicketChange,
// ports.SessionChange): { ticket, deleted? } and { session, deleted? }. The
// shared page stream carries them without an SSE name, so the key tells them
// from the feed's other events (a `task_status` is the board's, not the rail's).

import { DtoError } from "../../../../shared/infrastructure/dto/dto-error";
import { object, type Wire } from "../../../../shared/infrastructure/dto/fields";
import { RailSessionDto } from "./session-dto";
import { TaskDto } from "./task-dto";

const TICKET = "TicketChangeDto";
const SESSION = "SessionChangeDto";

function deleted(from: Wire): boolean {
  return from["deleted"] === true;
}

export class TicketChangeDto {
  constructor(
    readonly ticket: TaskDto,
    readonly deleted: boolean,
  ) {}

  static fromJSON(raw: unknown): TicketChangeDto {
    const from = object(TICKET, raw);
    if (!("ticket" in from)) throw new DtoError(TICKET, "ticket", "expected a ticket");
    return new TicketChangeDto(TaskDto.fromJSON(from["ticket"], "ticket"), deleted(from));
  }

  toJSON(): Wire {
    return { ticket: this.ticket.toJSON(), ...(this.deleted ? { deleted: true } : {}) };
  }
}

export class SessionChangeDto {
  constructor(
    readonly session: RailSessionDto,
    readonly deleted: boolean,
  ) {}

  static fromJSON(raw: unknown): SessionChangeDto {
    const from = object(SESSION, raw);
    if (!("session" in from)) throw new DtoError(SESSION, "session", "expected a session");
    return new SessionChangeDto(RailSessionDto.fromJSON(from["session"], "session"), deleted(from));
  }

  toJSON(): Wire {
    return { session: this.session.toJSON(), ...(this.deleted ? { deleted: true } : {}) };
  }
}
