// How a notification reads on screen: the row the panel and the inbox page
// render, and the bell's words. Plain functions; the components bind to what
// they return (the CSP build evaluates no expressions).

import { relativeTime } from "../../../shared/presentation/format.js";
import { isSettled, isUnread } from "../domain/notification.js";

/** The badge stops counting here. */
const BADGE_MAX = 99;

/** The badge's text: "", "3", "99+". */
export function badgeText(count) {
  if (!count) return "";
  return count > BADGE_MAX ? `${BADGE_MAX}+` : String(count);
}

/** The bell's accessible name, with the exact count. */
export function bellLabel(count) {
  if (!count) return "Notifications";
  return `Notifications, ${count} unread`;
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

/**
 * @param {import("../domain/notification.js").Notification} n
 * @param {Date} now
 * @returns {Row}
 */
export function toRow(n, now) {
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
    hasTask: n.taskTitle !== "",
    level: n.level,
    levelLabel,
    unread,
    settled,
    state: unread ? "unread" : settled ? "settled" : "read",
    age: relativeTime(n.createdAt, now),
    at: n.createdAt.toISOString(),
    markLabel: `Mark “${n.title}” read`,
    sessions: n.interruptedSessions.map(sessionLine),
    hasSessions: n.interruptedSessions.length > 0,
    canResumeAll: n.kind === "sessions_interrupted" && !settled,
    label: [unread ? "Unread." : "", levelLabel ? `${levelLabel}.` : "", n.title, where ? `— ${where}` : ""].filter(Boolean).join(" "),
  };
}
