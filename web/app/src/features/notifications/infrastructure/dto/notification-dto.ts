// A notification as the server sends it (Contract: Harness server ↔ Web UI —
// notifications, §1). Fields keep the wire names; optional ones are undefined
// when absent.

import { DtoError } from "../../../../shared/infrastructure/dto/dto-error";
import { array, localPath, object, oneOf, optionalString, optionalTime, string, time, type Wire } from "../../../../shared/infrastructure/dto/fields";
import { INTERRUPTED_STATES, KINDS, LEVELS, SOURCES } from "../../domain/notification";

const SESSION = "InterruptedSessionDto";
const NOTIFICATION = "NotificationDto";

const iso = (date: Date | null): string | undefined => date?.toISOString();

/** One session a restart interrupted (a sessions_interrupted notification's list). */
export class InterruptedSessionDto {
  constructor(
    readonly session_id: string,
    readonly project_id: string | undefined,
    readonly task_id: string | undefined,
    readonly task_title: string | undefined,
    readonly agent_name: string | undefined,
    readonly link: string,
    readonly state: (typeof INTERRUPTED_STATES)[number],
    readonly reason: string | undefined,
  ) {}

  static fromJSON(raw: unknown, at = ""): InterruptedSessionDto {
    const from = object(SESSION, raw, at);
    return new InterruptedSessionDto(
      string(SESSION, from, "session_id", at),
      optionalString(SESSION, from, "project_id", at),
      optionalString(SESSION, from, "task_id", at),
      optionalString(SESSION, from, "task_title", at),
      optionalString(SESSION, from, "agent_name", at),
      localPath(SESSION, from, "link", at),
      oneOf(SESSION, from, "state", INTERRUPTED_STATES, at),
      optionalString(SESSION, from, "reason", at),
    );
  }

  toJSON(): Wire {
    return { session_id: this.session_id, project_id: this.project_id, task_id: this.task_id, task_title: this.task_title, agent_name: this.agent_name, link: this.link, state: this.state, reason: this.reason };
  }
}

export class NotificationDto {
  constructor(
    readonly id: string,
    readonly project_id: string | undefined,
    readonly project_name: string | undefined,
    readonly task_id: string | undefined,
    readonly task_title: string | undefined,
    readonly session_id: string | undefined,
    readonly source: (typeof SOURCES)[number],
    readonly kind: (typeof KINDS)[number],
    readonly level: (typeof LEVELS)[number],
    readonly title: string | undefined,
    readonly body: string | undefined,
    readonly link: string,
    readonly created_at: Date,
    readonly read_at: Date | null,
    readonly resolved_at: Date | null,
    readonly interrupted_sessions: readonly InterruptedSessionDto[],
  ) {}

  static fromJSON(raw: unknown, at = ""): NotificationDto {
    const from = object(NOTIFICATION, raw, at);
    const id = string(NOTIFICATION, from, "id", at);
    if (id === "") throw new DtoError(NOTIFICATION, at === "" ? "id" : `${at}.id`, "expected a non-empty string");
    return new NotificationDto(
      id,
      optionalString(NOTIFICATION, from, "project_id", at),
      optionalString(NOTIFICATION, from, "project_name", at),
      optionalString(NOTIFICATION, from, "task_id", at),
      optionalString(NOTIFICATION, from, "task_title", at),
      optionalString(NOTIFICATION, from, "session_id", at),
      oneOf(NOTIFICATION, from, "source", SOURCES, at),
      oneOf(NOTIFICATION, from, "kind", KINDS, at),
      oneOf(NOTIFICATION, from, "level", LEVELS, at),
      optionalString(NOTIFICATION, from, "title", at),
      optionalString(NOTIFICATION, from, "body", at),
      localPath(NOTIFICATION, from, "link", at),
      time(NOTIFICATION, from, "created_at", at),
      optionalTime(NOTIFICATION, from, "read_at", at),
      optionalTime(NOTIFICATION, from, "resolved_at", at),
      from.interrupted_sessions === undefined ? [] : array(NOTIFICATION, from, "interrupted_sessions", (item, path) => InterruptedSessionDto.fromJSON(item, path), at),
    );
  }

  toJSON(): Wire {
    return {
      id: this.id,
      project_id: this.project_id,
      project_name: this.project_name,
      task_id: this.task_id,
      task_title: this.task_title,
      session_id: this.session_id,
      source: this.source,
      kind: this.kind,
      level: this.level,
      title: this.title,
      body: this.body,
      link: this.link,
      created_at: this.created_at.toISOString(),
      read_at: iso(this.read_at),
      resolved_at: iso(this.resolved_at),
      interrupted_sessions: this.interrupted_sessions.length > 0 ? this.interrupted_sessions.map((s) => s.toJSON()) : undefined,
    };
  }
}
