// Values for tests.

import type { FailureItem, MessageItem, ThreadItem, ThreadSummary } from "../domain/thread";

let counter = 0;
const at = (): Date => new Date(Date.UTC(2026, 10, 23, 8, 0, ++counter));

export const message = (over: Partial<MessageItem> = {}): MessageItem => ({ kind: "message", itemId: `m${++counter}`, at: at(), author: "product_owner", text: "Child 1 is in review.", streaming: false, stopped: false, ...over });
export const person = (text: string, over: Partial<MessageItem> = {}): MessageItem => message({ author: "person", text, ...over });
export const failure = (over: Partial<FailureItem> = {}): FailureItem => ({ kind: "failure", itemId: `f${++counter}`, at: at(), code: "SESSION_RESUME_FAILED", text: "The session did not wake.", retryItemId: "m1", ...over });
export const summary = (over: Partial<ThreadSummary> = {}): ThreadSummary => ({
  threadId: `t${++counter}`,
  title: "What was done this week",
  startedAt: at(),
  lastActivityAt: at(),
  lastMessageExcerpt: "Child 1 is in review; two sessions…",
  titleSetByPerson: false,
  turn: "idle",
  pendingHeldActions: 0,
  ...over,
});
export const itemOf = (i: ThreadItem): ThreadItem => i;
