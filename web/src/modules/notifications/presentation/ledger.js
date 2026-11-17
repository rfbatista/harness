// How the inbox page lays its notifications out (ADR 0095): the tabs, the
// pinned Needs you group and the day groups. Plain functions over domain
// notifications; the page component binds to what they return.

import { isOpen, isUnread } from "../domain/notification.js";

export const TABS = Object.freeze(["needs", "unread", "all"]);

/** The tab the page lands on: Needs you while anything is open, All otherwise. */
export const defaultTab = (openCount) => (openCount > 0 ? "needs" : "all");

/** A tab from the URL; anything else is no tab. */
export const tabFrom = (value) => (TABS.includes(value) ? value : "");

/** The list filter a tab asks the server for. */
export function filterOf(tab, projectId = "") {
  return { projectId, unread: tab === "unread", open: tab === "needs" };
}

const sameDay = (a, b) => a.getFullYear() === b.getFullYear() && a.getMonth() === b.getMonth() && a.getDate() === b.getDate();
const dayKey = (d) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;

/** "Today", "Yesterday", then "Sun 9 Nov" (with the year when it is not this one). */
export function dayLabel(date, now) {
  if (sameDay(date, now)) return "Today";
  const yesterday = new Date(now.getFullYear(), now.getMonth(), now.getDate() - 1);
  if (sameDay(date, yesterday)) return "Yesterday";
  const part = (o) => date.toLocaleDateString("en-GB", o);
  const label = `${part({ weekday: "short" })} ${part({ day: "numeric" })} ${part({ month: "short" })}`;
  return date.getFullYear() === now.getFullYear() ? label : `${label} ${date.getFullYear()}`;
}

/**
 * The groups a tab shows: the open causes pinned as Needs you, then the rest
 * by local day. Each group's items keep their order (newest first). The
 * Needs you tab is that one group: what it holds was open when it was read,
 * and one that settles meanwhile stays, settled, until the list is read again
 * (as a read row stays in the Unread list).
 * @param {import("../domain/notification.js").Notification[]} items
 * @param {"needs"|"unread"|"all"} tab
 * @param {Date} now
 * @returns {{ key: string, title: string, tone: string, items: import("../domain/notification.js").Notification[] }[]}
 */
export function groupsFor(items, tab, now) {
  if (tab === "needs") return items.length ? [{ key: "needs", title: "Needs you", tone: "attention", items }] : [];
  const open = items.filter(isOpen);
  const groups = [];
  if (open.length) groups.push({ key: "needs", title: "Needs you", tone: "attention", items: open });
  for (const n of items) {
    if (isOpen(n)) continue;
    const key = dayKey(n.createdAt);
    const last = groups.at(-1);
    if (last && last.key === `day:${key}`) last.items.push(n);
    else groups.push({ key: `day:${key}`, title: dayLabel(n.createdAt, now), tone: "", items: [n] });
  }
  return groups;
}

/**
 * The tab counts: what needs the person and what is unread, as the server
 * counts them (or, with no server count, as the items held do); All shows its
 * count only when it is the tab shown and every notification is held.
 * @returns {{ needs: number, unread: number, all: number | null }}
 */
export function tabCounts({ items, openCount, unreadCount, hasMore, tab }) {
  return {
    needs: openCount ?? items.filter(isOpen).length,
    unread: unreadCount ?? items.filter(isUnread).length,
    all: tab === "all" && !hasMore ? items.length : null,
  };
}
