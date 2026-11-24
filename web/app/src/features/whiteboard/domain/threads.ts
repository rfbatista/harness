// What passes between sessions: counts, the latest message and what waits for a reply.

import type { MessageKind, ReviewRequest, TaskMessage } from "./message";

export const KIND_WORD: Record<MessageKind, string> = {
  review_request: "review request",
  status_report: "report",
  question: "question",
  reply: "reply",
};

/** A question or review request waits while no message replies to it. */
export function pendingMessages(messages: readonly TaskMessage[]): TaskMessage[] {
  const replied = new Set(messages.filter((m) => m.inReplyTo !== "").map((m) => m.inReplyTo));
  return messages.filter((m) => (m.kind === "question" || m.kind === "review_request") && !replied.has(m.id));
}

export interface Thread {
  /** Messages between the two sessions, both directions. */
  count: number;
  latest: TaskMessage | null;
  /** The oldest message of the pair that still waits for a reply. */
  pending: TaskMessage | null;
}

const byTime = (a: TaskMessage, b: TaskMessage): number => a.createdAt.getTime() - b.createdAt.getTime() || (a.id < b.id ? -1 : 1);

/** Everything per started edge key "from>to": built once from all messages and reused. */
export function threadsOf(messages: readonly TaskMessage[]): Map<string, Thread> {
  const waiting = new Set(pendingMessages(messages).map((m) => m.id));
  const threads = new Map<string, Thread>();
  for (const m of [...messages].sort(byTime)) {
    const key = pairKey(m.fromSessionId, m.toSessionId);
    const thread = threads.get(key) ?? { count: 0, latest: null, pending: null };
    thread.count += 1;
    thread.latest = m;
    if (waiting.has(m.id) && !thread.pending) thread.pending = m;
    threads.set(key, thread);
  }
  return threads;
}

/** The same key whichever side sent. */
export const pairKey = (a: string, b: string): string => (a < b ? `${a}|${b}` : `${b}|${a}`);

/** A delegate's latest status report. */
export function lastReportOf(messages: readonly TaskMessage[], sessionId: string): TaskMessage | null {
  let latest: TaskMessage | null = null;
  for (const m of messages) {
    if (m.fromSessionId === sessionId && m.kind === "status_report" && (!latest || byTime(latest, m) <= 0)) latest = m;
  }
  return latest;
}

/** "less than a minute", "1 minute", "11 minutes", "3 hours", "2 days": for a sentence. */
export function ageWords(then: Date, now: Date): string {
  const minutes = Math.floor(Math.max(0, now.getTime() - then.getTime()) / 60_000);
  if (minutes < 1) return "less than a minute";
  const [n, unit] = minutes < 60 ? [minutes, "minute"] : minutes < 1440 ? [Math.floor(minutes / 60), "hour"] : [Math.floor(minutes / 1440), "day"];
  return `${n} ${unit}${n === 1 ? "" : "s"}`;
}

export interface ReviewWait {
  count: number;
  oldest: Date;
}

/** The review requests waiting on the person, per architect session. */
export function reviewWaits(reviews: readonly ReviewRequest[]): Map<string, ReviewWait> {
  const waits = new Map<string, ReviewWait>();
  for (const r of reviews) {
    if (r.state !== "pending") continue;
    const w = waits.get(r.architectSessionId);
    if (!w) waits.set(r.architectSessionId, { count: 1, oldest: r.createdAt });
    else {
      w.count += 1;
      if (r.createdAt < w.oldest) w.oldest = r.createdAt;
    }
  }
  return waits;
}
