// The notification DTOs as domain values, and the domain's requests as DTOs.

import type { ReadRequest } from "../application/ports";
import type { Change, Notification, NotificationPage } from "../domain/notification";
import type { NotificationDto } from "./dto/notification-dto";
import type { ChangeEventDto, NotificationPageDto, ReadAnswerDto } from "./dto/notification-page-dto";
import { ReadRequestDto } from "./dto/read-request-dto";

export function toNotification(dto: NotificationDto): Notification {
  return {
    id: dto.id,
    projectId: dto.project_id ?? "",
    projectName: dto.project_name ?? "",
    taskId: dto.task_id ?? "",
    taskTitle: dto.task_title ?? "",
    sessionId: dto.session_id ?? "",
    source: dto.source,
    kind: dto.kind,
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
  };
}

export function toPage(dto: NotificationPageDto): NotificationPage {
  return { notifications: dto.notifications.map(toNotification), unreadCount: dto.unread_count, nextBefore: dto.next_before ?? "" };
}

export function toChange(dto: ReadAnswerDto | ChangeEventDto): Change {
  return { notifications: ("changed" in dto ? dto.changed : dto.notifications).map(toNotification), unreadCount: dto.unread_count };
}

export function toReadRequestDto(request: ReadRequest): ReadRequestDto {
  if ("ids" in request) return new ReadRequestDto(request.ids);
  if ("all" in request) return new ReadRequestDto(undefined, true);
  return new ReadRequestDto();
}
