// Values for tests.

import type { FailureItem, MemoryEntry, MemoryNoteItem, MessageItem, ThreadItem, ThreadSummary } from "../domain/thread";

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
export const entry = (over: Partial<MemoryEntry> = {}): MemoryEntry => ({ entryId: `e${++counter}`, text: "The release is planned for 20 Nov.", savedAt: at(), changedAt: null, changedBy: null, threadId: "t1", threadTitle: "What blocks the release", ...over });
export const memoryNote = (over: Partial<MemoryNoteItem> = {}): MemoryNoteItem => ({ kind: "memory_note", itemId: `n${++counter}`, at: at(), op: "saved", entryId: "e1", text: "The release is planned for 20 Nov.", ...over });
