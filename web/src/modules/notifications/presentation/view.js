// How a notification reads on screen: the row the panel and the inbox page
// render, and the bell's words. Plain functions; the components bind to what
// they return (the CSP build evaluates no expressions).

import { agentSlot } from "../../architecture/domain/layers.js";
import { relativeTime } from "../../../shared/presentation/format.js";
import { Codes, codeOf } from "../../../shared/domain/errors.js";
import { FeedStatus } from "../../../shared/domain/feed.js";
import { isSettled, isUnread } from "../domain/notification.js";

/** The badge stops counting here. */
const BADGE_MAX = 99;

/** The badge's text: "", "3", "99+". */
export function badgeText(count) {
  if (!count) return "";
  return count > BADGE_MAX ? `${BADGE_MAX}+` : String(count);
}

/** The bell's accessible name, with the exact count and what it counts. */
export function bellLabel(count, countsOpen = false, unread = 0) {
  if (!count) return "Notifications";
  if (!countsOpen) return `Notifications, ${count} unread`;
  return `Notifications, ${count} need you${unread ? `, ${unread} unread` : ""}`;
}

/**
 * @typedef {object} Row
 * @property {string} id
 * @property {string} href
 * @property {string} title
 * @property {string} body
 * @property {string} project
 * @property {string} task         "" when the task is gone or there is none
 * @property {boolean} hasTask
 * @property {"info"|"action"} level
 * @property {string} kind
 * @property {string} repository
 * @property {boolean} hasRepository
 * @property {string} whoName      the agent as its session row shows it, or the harness's own source
 * @property {string} whoMode      the session's mode word; "" for the system
 * @property {boolean} hasWhoMode
 * @property {string} whoInitial
 * @property {string} whoSlot      the agent's colour slot ("0".."7"); "" for the system
 * @property {boolean} whoIsSystem
 * @property {"agent"|"system"} whoKind
 * @property {boolean} hasVerb     false on kind agent and on revision 5 data: the what is the title
 * @property {string} verb
 * @property {string} objectText
 * @property {boolean} hasObject
 * @property {boolean} objectIsCode
 * @property {string} clock        "15:40"
 * @property {string} fullDate     "Mon 17 Nov 2026, 15:40"
 * @property {string} statusText   "Needs you", "Approved 15:40", "Settled", "Failed" or ""
 * @property {boolean} hasStatus
 * @property {"attention"|"danger"|"success"|"quiet"} statusTone
 * @property {string} levelLabel   "Needs you", "Settled", or ""
 * @property {boolean} unread
 * @property {boolean} settled
 * @property {string} state        "unread" | "read" | "settled", for data-state
 * @property {string} age          "5m ago"
 * @property {string} at           ISO time, for <time datetime>
 * @property {string} markLabel    the row's Mark read button, named for the row
 * @property {string} label        the link's accessible name
 * @property {SessionLine[]} sessions  a sessions_interrupted item's list; [] for the others
 * @property {boolean} hasSessions
 * @property {boolean} canResumeAll    a sessions_interrupted item whose cause is not settled
 *
 * @typedef {object} SessionLine
 * @property {string} key
 * @property {string} label        "Ship the thing · go-developer"
 * @property {string} href
 * @property {string} state        interrupted | resumed | failed | gone
 * @property {string} reason       why the last resume failed, "" otherwise
 * @property {boolean} hasReason
 */

/** One session of a sessions_interrupted item: who it was, and where it stands. */
function sessionLine(s) {
  return {
    key: s.sessionId,
    label: [s.taskTitle || "No task", s.agentName].filter(Boolean).join(" · "),
    href: s.link,
    state: s.state,
    reason: s.reason,
    hasReason: s.reason !== "",
  };
}

const OUTCOME_WORDS = Object.freeze({
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
});

/** "15:40", in the person's time zone. */
const clockTime = (d) => `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;

/** "Mon 17 Nov 2026, 15:40": the tooltip and the accessible name. */
const fullDate = (d) => `${d.toLocaleDateString("en-GB", { weekday: "short", day: "numeric", month: "short", year: "numeric" })}, ${clockTime(d)}`;

/** Where the item stands: the words and their tone ("" for no line at all). */
function standing(n, settled) {
  if (!settled) {
    if (n.kind === "session_failed") return ["Failed", "danger"];
    return n.level === "action" ? ["Needs you", "attention"] : ["", "quiet"];
  }
  const word = OUTCOME_WORDS[n.outcome];
  return word ? [`${word[0]} ${clockTime(n.resolvedAt)}`, word[1]] : ["Settled", "quiet"];
}

/**
 * @param {import("../domain/notification.js").Notification} n
 * @param {Date} now
 * @returns {Row}
 */
export function toRow(n, now) {
  const unread = isUnread(n);
  const settled = isSettled(n);
  const [statusText, statusTone] = standing(n, settled);
  const needsYou = !settled && n.level === "action";
  const hasVerb = n.verb !== "";
  const what = hasVerb ? [n.verb, n.object?.text].filter(Boolean).join(" ") : n.title;
  const where = [[n.projectName, n.taskTitle].filter(Boolean).join(" › "), n.repositoryName].filter(Boolean).join(" · ");
  const age = relativeTime(n.createdAt, now);
  const when = fullDate(n.createdAt);
  const isSystem = n.actor.kind !== "agent";
  return {
    id: n.id,
    kind: n.kind,
    href: n.link,
    title: n.title,
    body: n.body,
    project: n.projectName,
    task: n.taskTitle,
    hasTask: n.taskTitle !== "",
    repository: n.repositoryName,
    hasRepository: n.repositoryName !== "",
    level: n.level,
    levelLabel: settled ? "Settled" : needsYou ? "Needs you" : "",
    whoName: n.actor.name,
    whoMode: n.actor.mode,
    hasWhoMode: n.actor.mode !== "",
    whoInitial: Array.from(n.actor.name)[0] ?? "",
    whoSlot: isSystem ? "" : String(agentSlot(n.actor.name)),
    whoIsSystem: isSystem,
    whoKind: isSystem ? "system" : "agent",
    hasVerb,
    verb: n.verb,
    objectText: n.object?.text ?? "",
    hasObject: n.object !== null,
    objectIsCode: n.object?.code === true,
    clock: clockTime(n.createdAt),
    fullDate: when,
    statusText,
    hasStatus: statusText !== "",
    statusTone,
    unread,
    settled,
    state: unread ? "unread" : settled ? "settled" : "read",
    age,
    at: n.createdAt.toISOString(),
    markLabel: `Mark “${what}” read`,
    sessions: n.interruptedSessions.map(sessionLine),
    hasSessions: n.interruptedSessions.length > 0,
    canResumeAll: n.kind === "sessions_interrupted" && !settled,
    label: [unread ? "Unread." : "", needsYou ? "Needs you." : "", `${n.actor.name} ${what}.`, where ? `${where}.` : "", `${when}, ${age === "now" ? "just now" : `${age} ago`}.`].filter(Boolean).join(" "),
  };
}

/**
 * What a failed load or action reads as: a plain sentence, no code and no
 * developer message. The banner pairs it with a Reload action.
 * @param {unknown} err
 * @returns {{ message: string }}
 */
export function inboxError(err) {
  switch (codeOf(err)) {
    case Codes.NETWORK:
      return { message: "The notifications could not be loaded. The harness server did not answer." };
    case Codes.BAD_RESPONSE:
      return { message: "The notifications could not be read. Reload; if it stays, check the harness server log." };
    default:
      return { message: "The notifications could not be loaded." };
  }
}

/** The line a dropped stream shows; "" while the stream is fine. */
export function streamNotice(status) {
  return status === FeedStatus.PAUSED ? "Live updates are paused. Reconnecting; Reload shows what is there now." : "";
}
