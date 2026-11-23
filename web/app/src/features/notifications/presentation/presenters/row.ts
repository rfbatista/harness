// How a notification reads on screen: the ledger row of the panel (ADR 0097),
// the same words as the Alpine client's rows.

import { relativeTime } from "../../../../shared/kernel/format";
import { agentSlot } from "../../domain/agent-slot";
import { isSettled, isUnread, type Notification, type Outcome } from "../../domain/notification";

export interface SessionLine {
  readonly key: string;
  /** "Ship the thing · go-developer" */
  readonly label: string;
  readonly href: string;
  readonly state: string;
  readonly reason: string;
}

export interface ListedLine {
  readonly key: string;
  readonly label: string;
  readonly href: string;
  readonly state: string;
  /** "check off", "repaired" or "ended". */
  readonly stateWord: string;
}

export type StatusTone = "attention" | "danger" | "success" | "quiet";

export interface Row {
  readonly id: string;
  readonly kind: string;
  readonly href: string;
  readonly title: string;
  readonly body: string;
  readonly project: string;
  /** "" when the task is gone or there is none. */
  readonly task: string;
  readonly repository: string;
  readonly level: "info" | "action";
  /** The agent as its session row shows it, or the harness's own source. */
  readonly whoName: string;
  /** The session's mode word; "" for the system. */
  readonly whoMode: string;
  readonly whoInitial: string;
  /** The agent's colour slot ("0".."7"); "" for the system. */
  readonly whoSlot: string;
  readonly whoKind: "agent" | "system" | "person";
  /** False on kind agent and on data without a verb: the what is the title. */
  readonly hasVerb: boolean;
  readonly verb: string;
  readonly objectText: string;
  readonly hasObject: boolean;
  readonly objectIsCode: boolean;
  /** "15:40", in the person's time zone; "" until the panel knows the time. */
  readonly clock: string;
  /** "Mon 17 Nov 2026, 15:40"; "" until the panel knows the time. */
  readonly fullDate: string;
  /** "Needs you", "Approved 15:40", "Settled", "Failed" or "". */
  readonly statusText: string;
  readonly statusTone: StatusTone;
  readonly unread: boolean;
  /** "unread" | "read" | "settled", for data-state. */
  readonly state: string;
  /** "5m"; "" until the panel knows the time (the server render and the first client render agree on it). */
  readonly age: string;
  /** ISO time, for <time datetime>. */
  readonly at: string;
  /** The row's Mark read button, named for the row. */
  readonly markLabel: string;
  /** The link's accessible name. */
  readonly label: string;
  /** A sessions_interrupted item's list; empty for the others. */
  readonly sessions: readonly SessionLine[];
  readonly listed: readonly ListedLine[];
  /** The plain-text body shows unless a list stands for it. */
  readonly showBody: boolean;
}

const OUTCOME_WORDS: Record<Outcome, readonly [string, StatusTone]> = {
  approved: ["Approved", "success"],
  changes_requested: ["Changes requested", "attention"],
  denied: ["Denied", "danger"],
  expired: ["Expired", "quiet"],
  withdrawn: ["Withdrawn", "quiet"],
  answered: ["Answered", "success"],
  dismissed: ["Dismissed", "quiet"],
  acknowledged: ["Acknowledged", "success"],
  repaired: ["Repaired", "success"],
  resumed: ["Resumed", "success"],
  ended: ["Ended", "quiet"],
  done: ["Done", "success"],
  reopened: ["Reopened", "attention"],
};

const LISTED_WORDS: Record<string, string> = { off: "check off", repaired: "repaired", ended: "ended" };

const clockTime = (d: Date): string => `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;

/** "Mon 17 Nov 2026, 15:40": the tooltip and the accessible name. */
function fullDate(d: Date): string {
  const part = (o: Intl.DateTimeFormatOptions) => d.toLocaleDateString("en-GB", o);
  return `${part({ weekday: "short" })} ${part({ day: "numeric" })} ${part({ month: "short" })} ${d.getFullYear()}, ${clockTime(d)}`;
}

/** Where the item stands: the words and their tone ("" for no line at all). */
function standing(n: Notification, settled: boolean): readonly [string, StatusTone] {
  if (!settled) {
    if (n.kind === "session_failed") return ["Failed", "danger"];
    return n.level === "action" ? ["Needs you", "attention"] : ["", "quiet"];
  }
  const word = n.outcome === "" ? undefined : OUTCOME_WORDS[n.outcome];
  return word && n.resolvedAt ? [`${word[0]} ${clockTime(n.resolvedAt)}`, word[1]] : ["Settled", "quiet"];
}

const sessionLabel = (taskTitle: string, agentName: string): string => [taskTitle || "No task", agentName].filter(Boolean).join(" · ");

/** `now` is null until the view has mounted: the time-dependent words are then empty, because the server (another time zone, another time) could not match them. */
export function toRow(n: Notification, now: Date | null): Row {
  const unread = isUnread(n);
  const settled = isSettled(n);
  const [statusText, statusTone] = standing(n, settled);
  const needsYou = !settled && n.level === "action";
  const hasVerb = n.verb !== "";
  const what = hasVerb ? [n.verb, n.object?.text].filter(Boolean).join(" ") : n.title;
  const where = [[n.projectName, n.taskTitle].filter(Boolean).join(" › "), n.repositoryName].filter(Boolean).join(" · ");
  const age = now ? relativeTime(n.createdAt, now) : "";
  const when = now ? fullDate(n.createdAt) : "";
  return {
    id: n.id,
    kind: n.kind,
    href: n.link,
    title: n.title,
    body: n.body,
    project: n.projectName,
    task: n.taskTitle,
    repository: n.repositoryName,
    level: n.level,
    whoName: n.actor.name,
    whoMode: n.actor.mode,
    whoInitial: Array.from(n.actor.name)[0] ?? "",
    whoSlot: n.actor.kind === "agent" ? String(agentSlot(n.actor.name)) : "",
    whoKind: n.actor.kind,
    hasVerb,
    verb: n.verb,
    objectText: n.object?.text ?? "",
    hasObject: n.object !== null,
    objectIsCode: n.object?.code === true,
    clock: now ? clockTime(n.createdAt) : "",
    fullDate: when,
    statusText,
    statusTone,
    unread,
    state: unread ? "unread" : settled ? "settled" : "read",
    age,
    at: n.createdAt.toISOString(),
    markLabel: `Mark “${what}” read`,
    sessions: n.interruptedSessions.map((s) => ({ key: s.sessionId, label: sessionLabel(s.taskTitle, s.agentName), href: s.link, state: s.state, reason: s.reason })),
    listed: n.listedSessions.map((s) => ({ key: s.sessionId, label: sessionLabel(s.taskTitle, s.agentName), href: s.link, state: s.state, stateWord: LISTED_WORDS[s.state] ?? s.state })),
    showBody: n.body !== "" && n.interruptedSessions.length === 0 && n.listedSessions.length === 0,
    label: [unread ? "Unread." : "", needsYou ? "Needs you." : "", `${n.actor.name} ${what}.`, where ? `${where}.` : "", now ? `${when}, ${age === "now" ? "just now" : `${age} ago`}.` : ""].filter(Boolean).join(" "),
  };
}
