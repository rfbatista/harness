// The notification DTOs as domain values, and the domain's requests as DTOs.

import type { ReadRequest } from "../application/ports";
import { ACTOR_KINDS, HARNESS_ACTOR, KINDS, LISTED_STATES, OUTCOMES, type Actor, type Change, type Notification, type NotificationPage } from "../domain/notification";
import type { NotificationDto } from "./dto/notification-dto";
import type { ChangeEventDto, NotificationPageDto, ReadAnswerDto } from "./dto/notification-page-dto";
import { ReadRequestDto } from "./dto/read-request-dto";

const oneOf = <T extends string>(allowed: readonly T[], value: string | undefined): T | undefined => allowed.find((a) => a === value);

// A future kind of actor is still someone: it reads as the harness's own.
function toActor(dto: NotificationDto["actor"]): Actor {
  if (!dto || dto.name === "") return HARNESS_ACTOR;
  return { kind: oneOf(ACTOR_KINDS, dto.kind) ?? "system", name: dto.name, mode: dto.mode ?? "", sessionId: dto.session_id ?? "" };
}

export function toNotification(dto: NotificationDto): Notification {
  return {
    id: dto.id,
    projectId: dto.project_id ?? "",
    projectName: dto.project_name ?? "",
    taskId: dto.task_id ?? "",
    taskTitle: dto.task_title ?? "",
    sessionId: dto.session_id ?? "",
    source: dto.source,
    // A kind this client does not know reads as a generic notification (its title, body and link), so one new kind never empties the inbox.
    kind: oneOf(KINDS, dto.kind) ?? "agent",
    level: dto.level,
    title: dto.title ?? "",
    body: dto.body ?? "",
    link: dto.link,
    createdAt: dto.created_at,
    readAt: dto.read_at,
    resolvedAt: dto.resolved_at,
    interruptedSessions: dto.interrupted_sessions.map((s) => ({
      sessionId: s.session_id,
      projectId: s.project_id ?? "",
      taskId: s.task_id ?? "",
      taskTitle: s.task_title ?? "",
      agentName: s.agent_name ?? "",
      link: s.link,
      state: s.state,
      reason: s.reason ?? "",
    })),
    actor: toActor(dto.actor),
    verb: dto.verb ?? "",
    object: dto.object && dto.object.text !== "" ? { text: dto.object.text, code: dto.object.code } : null,
    repositoryName: dto.repository_name ?? "",
    // An outcome this client does not know reads as settled with none, not as an error.
    outcome: oneOf(OUTCOMES, dto.outcome) ?? "",
    listedSessions: dto.listed_sessions.map((s) => ({
      sessionId: s.session_id,
      taskId: s.task_id ?? "",
      taskTitle: s.task_title ?? "",
      agentName: s.agent_name ?? "",
      link: s.link,
      state: oneOf(LISTED_STATES, s.state) ?? "off",
    })),
  };
}

export function toPage(dto: NotificationPageDto): NotificationPage {
  return { notifications: dto.notifications.map(toNotification), unreadCount: dto.unread_count, openCount: dto.open_count ?? null, nextBefore: dto.next_before ?? "" };
}

export function toChange(dto: ReadAnswerDto | ChangeEventDto): Change {
  return { notifications: ("changed" in dto ? dto.changed : dto.notifications).map(toNotification), unreadCount: dto.unread_count, openCount: dto.open_count ?? null };
}

export function toReadRequestDto(request: ReadRequest): ReadRequestDto {
  if ("ids" in request) return new ReadRequestDto(request.ids);
  if ("all" in request) return new ReadRequestDto(undefined, true);
  return new ReadRequestDto();
}
