// NotificationGateway in memory: the fake for page tests and the data source
// for web/dev pages. It obeys the same contract as the real gateway
// (../testing/gateway-contract.js), including the order the server keeps: a
// change is committed, then streamed, then answered.

import { Codes, StructuredError } from "../../../shared/domain/errors.js";
import { FeedStatus } from "../../../shared/domain/feed.js";
import { isUnread, markedRead, matches, upsert } from "../domain/notification.js";

const MAX_LIMIT = 200;
const DEFAULT_LIMIT = 50;

/**
 * @param {{
 *   notifications?: import("../domain/notification.js").Notification[],
 *   now?: () => Date,
 *   beforeAnswer?: () => Promise<void>,  awaited after a read is streamed, before it is answered
 * }} [world]
 */
export function memoryNotifications({ notifications = [], now = () => new Date(), beforeAnswer = async () => {} } = {}) {
  let list = upsert([], notifications);
  const followers = new Set();
  let nextId = 1;

  const unreadCount = () => list.filter(isUnread).length;

  function commit(changed) {
    if (changed.length === 0) return { notifications: [], unreadCount: unreadCount() };
    list = upsert(list, changed);
    const change = { notifications: changed, unreadCount: unreadCount() };
    for (const f of followers) f.onChange(change);
    return change;
  }

  /** @type {import("../domain/ports.js").NotificationGateway} */
  const gateway = {
    async list({ projectId = "", unread = false, before = "", limit = DEFAULT_LIMIT } = {}) {
      if (!Number.isInteger(limit) || limit < 1 || limit > MAX_LIMIT) throw new StructuredError(Codes.INVALID_INPUT, `limit must be 1..${MAX_LIMIT}`, 400);
      let rest = list.filter((n) => matches({ projectId, unread }, n));
      if (before) {
        const at = list.findIndex((n) => n.id === before);
        if (at < 0) throw new StructuredError(Codes.INVALID_INPUT, "unknown cursor", 400);
        rest = rest.filter((n) => list.indexOf(n) > at);
      }
      const page = rest.slice(0, limit);
      return {
        notifications: page,
        unreadCount: unreadCount(),
        nextBefore: rest.length > limit ? page.at(-1).id : "",
      };
    },

    async markRead(request) {
      let targets;
      if ("ids" in request && Array.isArray(request.ids) && request.ids.length > 0) {
        const missing = request.ids.find((id) => !list.some((n) => n.id === id));
        if (missing) throw new StructuredError(Codes.NOTIFICATION_NOT_FOUND, `notification ${missing} not found`, 404);
        targets = list.filter((n) => request.ids.includes(n.id));
      } else if ("all" in request && request.all === true) {
        targets = list.filter((n) => matches({ projectId: request.projectId ?? "" }, n));
      } else {
        throw new StructuredError(Codes.INVALID_INPUT, "give ids or all", 400);
      }
      const at = now();
      const change = commit(targets.filter(isUnread).map((n) => markedRead(n, at)));
      await beforeAnswer();
      return change;
    },

    follow(onChange, onStatus) {
      const follower = { onChange };
      followers.add(follower);
      queueMicrotask(() => followers.has(follower) && onStatus(FeedStatus.LIVE));
      return () => followers.delete(follower);
    },
  };

  return {
    gateway,
    /** Something needs the person, or an agent called notify. */
    notify(fields = {}) {
      const n = Object.freeze({
        id: `n-${nextId++}`,
        projectId: "p1",
        projectName: "harness",
        taskId: "t1",
        taskTitle: "Notification",
        sessionId: "",
        source: "system",
        kind: "review_requested",
        level: "action",
        title: "Review requested",
        body: "",
        link: "/projects/p1/tasks/t1",
        createdAt: now(),
        readAt: null,
        resolvedAt: null,
        ...fields,
      });
      commit([n]);
      return n;
    },
    /** Its cause was settled: the review answered, the session moved on. */
    resolve(id) {
      const n = list.find((x) => x.id === id);
      if (!n || n.resolvedAt) return null;
      const resolved = Object.freeze({ ...n, resolvedAt: now() });
      commit([resolved]);
      return resolved;
    },
    find: (id) => list.find((n) => n.id === id) ?? null,
    unreadCount,
  };
}
