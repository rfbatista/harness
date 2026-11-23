// A notification as the server sends it (Contract: Harness server ↔ Web UI —
// notifications, §1). Fields keep the wire names; optional ones are undefined
// when absent.

import { DtoError } from "../../../../shared/infrastructure/dto/dto-error";
import { lenient, lenientArray, localPath, object, oneOf, optionalString, optionalTime, string, time, type Wire } from "../../../../shared/infrastructure/dto/fields";
import { INTERRUPTED_STATES, LEVELS, SOURCES } from "../../domain/notification";
import { ActorDto, ListedSessionDto, ObjectDto } from "./ledger-dto";

const SESSION = "InterruptedSessionDto";
const NOTIFICATION = "NotificationDto";

const join = (at: string, key: string): string => (at === "" ? key : `${at}.${key}`);
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
    /** A kind this client does not know still reads (as a generic one): the mapper decides. */
    readonly kind: string,
    readonly level: (typeof LEVELS)[number],
    readonly title: string | undefined,
    readonly body: string | undefined,
    readonly link: string,
    readonly created_at: Date,
    readonly read_at: Date | null,
    readonly resolved_at: Date | null,
    readonly interrupted_sessions: readonly InterruptedSessionDto[],
    readonly actor: ActorDto | undefined,
    readonly verb: string | undefined,
    readonly object: ObjectDto | undefined,
    readonly repository_name: string | undefined,
    /** An outcome this client does not know reads as none: the mapper decides. */
    readonly outcome: string | undefined,
    readonly listed_sessions: readonly ListedSessionDto[],
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
      string(NOTIFICATION, from, "kind", at),
      oneOf(NOTIFICATION, from, "level", LEVELS, at),
      optionalString(NOTIFICATION, from, "title", at),
      optionalString(NOTIFICATION, from, "body", at),
      localPath(NOTIFICATION, from, "link", at),
      time(NOTIFICATION, from, "created_at", at),
      optionalTime(NOTIFICATION, from, "read_at", at),
      optionalTime(NOTIFICATION, from, "resolved_at", at),
      // One malformed entry of a session list is dropped, never the notification or the page.
      lenientArray(NOTIFICATION, from, "interrupted_sessions", (item, path) => InterruptedSessionDto.fromJSON(item, path), at),
      // The ledger fields are extras: one that does not read reads as absent (actor: the harness's own).
      from.actor === undefined || from.actor === null ? undefined : lenient(() => ActorDto.fromJSON(from.actor, join(at, "actor"))),
      optionalString(NOTIFICATION, from, "verb", at),
      from.object === undefined || from.object === null ? undefined : lenient(() => ObjectDto.fromJSON(from.object, join(at, "object"))),
      optionalString(NOTIFICATION, from, "repository_name", at),
      lenient(() => optionalString(NOTIFICATION, from, "outcome", at)),
      lenientArray(NOTIFICATION, from, "listed_sessions", (item, path) => ListedSessionDto.fromJSON(item, path), at),
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
      actor: this.actor?.toJSON(),
      verb: this.verb,
      object: this.object?.toJSON(),
      repository_name: this.repository_name,
      outcome: this.outcome,
      listed_sessions: this.listed_sessions.length > 0 ? this.listed_sessions.map((s) => s.toJSON()) : undefined,
    };
  }
}
