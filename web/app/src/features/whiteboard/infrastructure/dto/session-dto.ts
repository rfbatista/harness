// A session as GET /api/sessions and the project feed send it (Web UI contract:
// sessions). Only the fields a card draws are read; unknown ones are ignored.

import { DtoError } from "../../../../shared/infrastructure/dto/dto-error";
import { object, oneOf, optionalString, optionalTime, string, time, type Wire } from "../../../../shared/infrastructure/dto/fields";
import { ROLES, STATUSES, type Role, type Status } from "../../domain/session";

const DTO = "BoardSessionDto";

const optionalCount = (from: Wire, key: string, at: string): number => {
  const value = from[key];
  if (value === undefined || value === null) return 0;
  if (typeof value !== "number" || !Number.isInteger(value) || value < 0) throw new DtoError(DTO, at === "" ? key : `${at}.${key}`, "expected a count");
  return value;
};

export class BoardSessionDto {
  constructor(
    readonly id: string,
    readonly ticket_id: string | undefined,
    readonly agent_id: string | undefined,
    readonly mode: string | undefined,
    readonly model: string | undefined,
    readonly task: string | undefined,
    readonly status: Status,
    readonly role: Role,
    readonly parent_session_id: string | undefined,
    readonly brought_under: boolean,
    readonly branch: string | undefined,
    readonly pending_approvals: number,
    readonly open_interview_count: number,
    readonly interrupted_at: Date | null,
    readonly created_at: Date,
    readonly updated_at: Date,
  ) {}

  static fromJSON(raw: unknown, at = ""): BoardSessionDto {
    const from = object(DTO, raw, at);
    const role = optionalString(DTO, from, "role", at) ?? "";
    if (!(ROLES as readonly string[]).includes(role)) throw new DtoError(DTO, at === "" ? "role" : `${at}.role`, "expected one of architect, delegate");
    return new BoardSessionDto(
      string(DTO, from, "id", at),
      optionalString(DTO, from, "ticket_id", at),
      optionalString(DTO, from, "agent_id", at),
      optionalString(DTO, from, "mode", at),
      optionalString(DTO, from, "model", at),
      optionalString(DTO, from, "task", at),
      oneOf(DTO, from, "status", STATUSES, at),
      role as Role,
      optionalString(DTO, from, "parent_session_id", at),
      from.brought_under === true,
      optionalString(DTO, from, "branch", at),
      optionalCount(from, "pending_approvals", at),
      optionalCount(from, "open_interview_count", at),
      optionalTime(DTO, from, "interrupted_at", at),
      time(DTO, from, "created_at", at),
      time(DTO, from, "updated_at", at),
    );
  }

  toJSON(): Wire {
    return {
      id: this.id,
      ticket_id: this.ticket_id,
      agent_id: this.agent_id,
      mode: this.mode,
      model: this.model,
      task: this.task,
      status: this.status,
      role: this.role,
      parent_session_id: this.parent_session_id,
      brought_under: this.brought_under,
      branch: this.branch,
      pending_approvals: this.pending_approvals,
      open_interview_count: this.open_interview_count,
      interrupted_at: this.interrupted_at?.toISOString(),
      created_at: this.created_at.toISOString(),
      updated_at: this.updated_at.toISOString(),
    };
  }
}

/** GET /api/sessions → { sessions: [...] }. */
export class BoardSessionListDto {
  constructor(readonly sessions: BoardSessionDto[]) {}

  static fromJSON(raw: unknown): BoardSessionListDto {
    const from = object("BoardSessionListDto", raw);
    const list = from.sessions;
    if (!Array.isArray(list)) throw new DtoError("BoardSessionListDto", "sessions", "expected an array");
    return new BoardSessionListDto(list.map((item, i) => BoardSessionDto.fromJSON(item, `sessions[${i}]`)));
  }

  toJSON(): Wire {
    return { sessions: this.sessions.map((s) => s.toJSON()) };
  }
}
