// DTOs to the whiteboard's domain values.

import type { ReviewRequest, TaskMessage } from "../domain/message";
import type { BoardSession } from "../domain/session";
import type { BoardEvent } from "../application/ports";
import type { BoardEventDto } from "./dto/event-dto";
import type { ReviewRequestDto, TaskMessageDto } from "./dto/message-dto";
import type { BoardSessionDto } from "./dto/session-dto";

export function toSession(dto: BoardSessionDto): BoardSession {
  return {
    id: dto.id,
    ticketId: dto.ticket_id ?? "",
    agentId: dto.agent_id ?? "",
    mode: dto.mode ?? "",
    model: dto.model ?? "",
    task: dto.task ?? "",
    status: dto.status,
    role: dto.role,
    parentSessionId: dto.parent_session_id ?? "",
    broughtUnder: dto.brought_under,
    branch: dto.branch ?? "",
    pendingApprovals: dto.pending_approvals,
    openInterviewCount: dto.open_interview_count,
    interruptedAt: dto.interrupted_at,
    createdAt: dto.created_at,
    updatedAt: dto.updated_at,
  };
}

export function toMessage(dto: TaskMessageDto): TaskMessage {
  return {
    id: dto.id,
    taskId: dto.task_id ?? "",
    fromSessionId: dto.from_session_id ?? "",
    toSessionId: dto.to_session_id ?? "",
    kind: dto.kind,
    status: dto.status,
    body: dto.body ?? "",
    inReplyTo: dto.in_reply_to ?? "",
    createdAt: dto.created_at,
  };
}

export function toReview(dto: ReviewRequestDto): ReviewRequest {
  return { id: dto.id, taskId: dto.task_id ?? "", architectSessionId: dto.architect_session_id ?? "", aboutSessionId: dto.about_session_id ?? "", state: dto.state, createdAt: dto.created_at };
}

/** The board's event for a feed message, or null for one it does not draw. */
export function toEvent(dto: BoardEventDto): BoardEvent | null {
  if (dto.task_message) return { kind: "message", message: toMessage(dto.task_message) };
  if (dto.review_request) return { kind: "review", review: toReview(dto.review_request) };
  if (dto.session) return { kind: "session", session: toSession(dto.session) };
  if (dto.deleted && dto.deleted_id) return { kind: "session-deleted", id: dto.deleted_id };
  return null;
}
