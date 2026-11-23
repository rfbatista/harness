// What passes between the sessions of a task, and what waits on the person.

export const MESSAGE_KINDS = ["review_request", "status_report", "question", "reply"] as const;
export type MessageKind = (typeof MESSAGE_KINDS)[number];

export const REPORT_STATUSES = ["", "working", "blocked", "ready_for_review", "done"] as const;
export type ReportStatus = (typeof REPORT_STATUSES)[number];

export interface TaskMessage {
  id: string;
  fromSessionId: string;
  toSessionId: string;
  kind: MessageKind;
  status: ReportStatus;
  body: string;
  inReplyTo: string;
  createdAt: Date;
}

export const REVIEW_STATES = ["pending", "approved", "changes_requested", "withdrawn"] as const;
export type ReviewState = (typeof REVIEW_STATES)[number];

export interface ReviewRequest {
  id: string;
  architectSessionId: string;
  aboutSessionId: string;
  state: ReviewState;
  createdAt: Date;
}

/** Adds or replaces `message` by id. */
export function upsertMessage(list: readonly TaskMessage[], message: TaskMessage): TaskMessage[] {
  const i = list.findIndex((m) => m.id === message.id);
  if (i < 0) return [...list, message];
  const next = [...list];
  next[i] = message;
  return next;
}

/** Adds or replaces `review` by id. */
export function upsertReview(list: readonly ReviewRequest[], review: ReviewRequest): ReviewRequest[] {
  const i = list.findIndex((r) => r.id === review.id);
  if (i < 0) return [...list, review];
  const next = [...list];
  next[i] = review;
  return next;
}
