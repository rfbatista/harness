// DTOs to the whiteboard's domain values.

import type { BoardSession } from "../domain/session";
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
