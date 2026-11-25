// Wire DTOs to the rail's domain.

import type { RailSession } from "../domain/activity";
import type { Task } from "../domain/task";
import type { RailSeed } from "../application/ports";
import type { RailSeedDto } from "./dto/rail-seed-dto";
import type { RailSessionDto } from "./dto/session-dto";
import type { TaskDto } from "./dto/task-dto";

/** ms since the epoch; 0 when absent, unreadable or the server's zero time (year 1). */
function ms(s: string): number {
  const t = Date.parse(s);
  return t > 0 ? t : 0;
}

export function toTask(dto: TaskDto): Task {
  return {
    id: dto.id,
    projectId: dto.project_id,
    title: dto.title,
    status: dto.status,
    parentId: dto.parent_id,
    pendingReviews: dto.pending_reviews,
    updatedAt: ms(dto.updated_at),
    // A task that never moved entered its status when it was created.
    statusSince: ms(dto.status_since) || ms(dto.created_at),
    archivedAt: ms(dto.archived_at),
    mergeState: dto.merge_state ? { state: dto.merge_state.state, merged: dto.merge_state.merged, total: dto.merge_state.total } : null,
  };
}

export function toSession(dto: RailSessionDto): RailSession {
  return { id: dto.id, ticketId: dto.ticket_id, status: dto.status, pendingApprovals: dto.pending_approvals, openInterviewCount: dto.open_interview_count };
}

export function toSeed(dto: RailSeedDto): RailSeed {
  return { projectId: dto.project_id, tasks: dto.tasks.map(toTask), sessions: dto.sessions.map(toSession), archivedCount: dto.archived_count };
}
