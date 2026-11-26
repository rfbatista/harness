// One message of the project feed (GET /api/events?project_id=): a session
// change, a task message, or a review request. Kinds the board does not draw
// read as nothing at all, so the server may add kinds.

import { object, type Wire } from "../../../../shared/infrastructure/dto/fields";
import { MemoryKillDto, MemorySampleDto } from "../../../../shared/infrastructure/dto/memory-dto";
import { SessionBranchesDto } from "./branches-dto";
import { ReviewRequestDto, TaskMessageDto } from "./message-dto";
import { BoardSessionDto } from "./session-dto";

export class BoardEventDto {
  constructor(
    readonly session: BoardSessionDto | null,
    /** The session change says the session is gone. */
    readonly deleted: boolean,
    readonly deleted_id: string | null,
    readonly task_message: TaskMessageDto | null,
    readonly review_request: ReviewRequestDto | null,
    readonly session_branches: SessionBranchesDto | null = null,
    readonly memory: MemorySampleDto | null = null,
    readonly memory_killed: MemoryKillDto | null = null,
  ) {}

  static fromJSON(raw: unknown): BoardEventDto {
    const from = object("BoardEventDto", raw);
    if (from.task_message !== undefined && from.task_message !== null) return new BoardEventDto(null, false, null, TaskMessageDto.fromJSON(from.task_message, "task_message"), null);
    if (from.review_request !== undefined && from.review_request !== null) return new BoardEventDto(null, false, null, null, ReviewRequestDto.fromJSON(from.review_request, "review_request"));
    if (from.session_branches !== undefined && from.session_branches !== null) return new BoardEventDto(null, false, null, null, null, SessionBranchesDto.fromJSON(from.session_branches, "session_branches"));
    if (from.memory !== undefined && from.memory !== null) return new BoardEventDto(null, false, null, null, null, null, MemorySampleDto.fromJSON(from.memory, "memory"));
    if (from.memory_killed !== undefined && from.memory_killed !== null) return new BoardEventDto(null, false, null, null, null, null, null, MemoryKillDto.fromJSON(from.memory_killed, "memory_killed"));
    if (from.session !== undefined && from.session !== null) {
      if (from.deleted === true) {
        const gone = object("BoardEventDto", from.session, "session");
        return new BoardEventDto(null, true, typeof gone.id === "string" ? gone.id : null, null, null);
      }
      return new BoardEventDto(BoardSessionDto.fromJSON(from.session, "session"), false, null, null, null);
    }
    return new BoardEventDto(null, false, null, null, null);
  }

  toJSON(): Wire {
    return { session: this.session?.toJSON(), deleted: this.deleted, task_message: this.task_message?.toJSON(), review_request: this.review_request?.toJSON(), session_branches: this.session_branches?.toJSON() };
  }
}
