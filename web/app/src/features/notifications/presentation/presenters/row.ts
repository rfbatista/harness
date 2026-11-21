// How a notification reads on screen: the row of the panel.

import { relativeTime } from "../../../../shared/kernel/format";
import { isSettled, isUnread, type Notification } from "../../domain/notification";

export interface SessionLine {
  readonly key: string;
  /** "Ship the thing · go-developer" */
  readonly label: string;
  readonly href: string;
  readonly state: string;
  readonly reason: string;
}

export interface Row {
  readonly id: string;
  readonly href: string;
  readonly title: string;
  readonly body: string;
  readonly project: string;
  /** "" when the task is gone or there is none. */
  readonly task: string;
  readonly level: "info" | "action";
  /** "Needs you", "Settled", or "". */
  readonly levelLabel: string;
  readonly unread: boolean;
  /** "unread" | "read" | "settled", for data-state. */
  readonly state: string;
  /** "5m" */
  readonly age: string;
  /** ISO time, for <time datetime>. */
  readonly at: string;
  /** The row's Mark read button, named for the row. */
  readonly markLabel: string;
  /** The link's accessible name. */
  readonly label: string;
  /** A sessions_interrupted item's list; empty for the others. */
  readonly sessions: readonly SessionLine[];
}

export function toRow(n: Notification, now: Date): Row {
  const unread = isUnread(n);
  const settled = isSettled(n);
  const levelLabel = settled ? "Settled" : n.level === "action" ? "Needs you" : "";
  const where = [n.projectName, n.taskTitle].filter(Boolean).join(" · ");
  return {
    id: n.id,
    href: n.link,
    title: n.title,
    body: n.body,
    project: n.projectName,
    task: n.taskTitle,
    level: n.level,
    levelLabel,
    unread,
    state: unread ? "unread" : settled ? "settled" : "read",
    age: relativeTime(n.createdAt, now),
    at: n.createdAt.toISOString(),
    markLabel: `Mark “${n.title}” read`,
    sessions: n.interruptedSessions.map((s) => ({
      key: s.sessionId,
      label: [s.taskTitle || "No task", s.agentName].filter(Boolean).join(" · "),
      href: s.link,
      state: s.state,
      reason: s.reason,
    })),
    label: [unread ? "Unread." : "", levelLabel ? `${levelLabel}.` : "", n.title, where ? `— ${where}` : ""].filter(Boolean).join(" "),
  };
}
