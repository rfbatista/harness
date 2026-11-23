// A task message as GET /api/task_messages and the project feed's task_message
// event send it, and a review request as GET /api/review_requests and the
// review_request event send it.

import { DtoError } from "../../../../shared/infrastructure/dto/dto-error";
import { array, object, oneOf, optionalString, string, time, type Wire } from "../../../../shared/infrastructure/dto/fields";
import { MESSAGE_KINDS, REPORT_STATUSES, REVIEW_STATES, type MessageKind, type ReportStatus, type ReviewState } from "../../domain/message";

const MESSAGE = "TaskMessageDto";
const REVIEW = "ReviewRequestDto";

export class TaskMessageDto {
  constructor(
    readonly id: string,
    readonly task_id: string | undefined,
    readonly from_session_id: string | undefined,
    readonly to_session_id: string | undefined,
    readonly kind: MessageKind,
    readonly status: ReportStatus,
    readonly body: string | undefined,
    readonly in_reply_to: string | undefined,
    readonly created_at: Date,
  ) {}

  static fromJSON(raw: unknown, at = ""): TaskMessageDto {
    const from = object(MESSAGE, raw, at);
    const status = optionalString(MESSAGE, from, "status", at) ?? "";
    if (!(REPORT_STATUSES as readonly string[]).includes(status)) throw new DtoError(MESSAGE, at === "" ? "status" : `${at}.status`, "expected a report status");
    return new TaskMessageDto(
      string(MESSAGE, from, "id", at),
      optionalString(MESSAGE, from, "task_id", at),
      optionalString(MESSAGE, from, "from_session_id", at),
      optionalString(MESSAGE, from, "to_session_id", at),
      oneOf(MESSAGE, from, "kind", MESSAGE_KINDS, at),
      status as ReportStatus,
      optionalString(MESSAGE, from, "body", at),
      optionalString(MESSAGE, from, "in_reply_to", at),
      time(MESSAGE, from, "created_at", at),
    );
  }

  toJSON(): Wire {
    return { id: this.id, task_id: this.task_id, from_session_id: this.from_session_id, to_session_id: this.to_session_id, kind: this.kind, status: this.status, body: this.body, in_reply_to: this.in_reply_to, created_at: this.created_at.toISOString() };
  }
}

/** GET /api/task_messages → { messages: [...] }. */
export class TaskMessageListDto {
  constructor(readonly messages: TaskMessageDto[]) {}

  static fromJSON(raw: unknown): TaskMessageListDto {
    const from = object("TaskMessageListDto", raw);
    return new TaskMessageListDto(array("TaskMessageListDto", from, "messages", (item, at) => TaskMessageDto.fromJSON(item, at)));
  }

  toJSON(): Wire {
    return { messages: this.messages.map((m) => m.toJSON()) };
  }
}

export class ReviewRequestDto {
  constructor(
    readonly id: string,
    readonly task_id: string | undefined,
    readonly architect_session_id: string | undefined,
    readonly about_session_id: string | undefined,
    readonly state: ReviewState,
    readonly created_at: Date,
  ) {}

  static fromJSON(raw: unknown, at = ""): ReviewRequestDto {
    const from = object(REVIEW, raw, at);
    return new ReviewRequestDto(
      string(REVIEW, from, "id", at),
      optionalString(REVIEW, from, "task_id", at),
      optionalString(REVIEW, from, "architect_session_id", at),
      optionalString(REVIEW, from, "about_session_id", at),
      oneOf(REVIEW, from, "state", REVIEW_STATES, at),
      time(REVIEW, from, "created_at", at),
    );
  }

  toJSON(): Wire {
    return { id: this.id, task_id: this.task_id, architect_session_id: this.architect_session_id, about_session_id: this.about_session_id, state: this.state, created_at: this.created_at.toISOString() };
  }
}

/** GET /api/review_requests → { review_requests: [...] }. */
export class ReviewRequestListDto {
  constructor(readonly review_requests: ReviewRequestDto[]) {}

  static fromJSON(raw: unknown): ReviewRequestListDto {
    const from = object("ReviewRequestListDto", raw);
    return new ReviewRequestListDto(array("ReviewRequestListDto", from, "review_requests", (item, at) => ReviewRequestDto.fromJSON(item, at)));
  }

  toJSON(): Wire {
    return { review_requests: this.review_requests.map((r) => r.toJSON()) };
  }
}
