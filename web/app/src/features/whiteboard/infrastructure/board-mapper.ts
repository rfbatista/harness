// DTOs to the whiteboard's domain values.

import type { ReviewRequest, TaskMessage } from "../domain/message";
import type { BoardSession } from "../domain/session";
import type { ReviewRequestDto, TaskMessageDto } from "./dto/message-dto";
import type { BoardSessionDto } from "./dto/session-dto";

export function toSession(dto: BoardSessionDto): BoardSession {
  return {
    id: dto.id,
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
  return { id: dto.id, architectSessionId: dto.architect_session_id ?? "", aboutSessionId: dto.about_session_id ?? "", state: dto.state, createdAt: dto.created_at };
}
