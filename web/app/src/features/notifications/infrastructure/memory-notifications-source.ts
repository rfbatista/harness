// NotificationsSource in memory, for tests and for development without a
// server. It answers like the server: newest first, the global count whatever
// the filter, an opaque cursor, INVALID_INPUT for a bad limit, and a read that
// echoes on the stream to every follower.

import { NotificationsSourceError, type NotificationsSource, type ReadRequest } from "../application/ports";
import type { FeedStatus } from "../domain/feed-status";
import { isUnread, markedRead, type Change, type Notification, type NotificationPage } from "../domain/notification";

const DEFAULT_LIMIT = 50;
const MAX_LIMIT = 200;

export interface MemoryNotificationsSource extends NotificationsSource {
  /** The notifications the server has, in any order. */
  seed(notifications: readonly Notification[]): void;
  /** Every next call fails with this error until `heal()`. */
  failWith(error: NotificationsSourceError): void;
  heal(): void;
  /** The server's unread count now. */
  readonly unreadCount: number;
  /** The stream delivers `change` to every follower. */
  publish(change: Change): void;
  /** The stream's state changes. */
  setStatus(status: FeedStatus): void;
  /** How many followers the stream has. */
  readonly followers: number;
  /** When the stream echoes a read relative to its answer: "before" is the harder case. Default "after". */
  echo: "before" | "after";
  /** The time the server stamps on a read. */
  readAt: Date;
}

export function memoryNotificationsSource(): MemoryNotificationsSource {
  let all: Notification[] = [];
  let failure: NotificationsSourceError | null = null;
  const followers = new Set<{ onChange: (change: Change) => void; onStatus: (status: FeedStatus) => void }>();
  let status: FeedStatus = "connecting";
  const newestFirst = () => [...all].sort((a, b) => b.createdAt.getTime() - a.createdAt.getTime() || (a.id < b.id ? 1 : -1));
  const unreadCount = () => all.filter(isUnread).length;
  const send = (change: Change) => {
    for (const f of [...followers]) f.onChange(change);
  };

  const source: MemoryNotificationsSource = {
    echo: "after",
    readAt: new Date(Date.UTC(2026, 10, 21, 13)),
    seed: (notifications) => void (all = [...notifications]),
    failWith: (error) => void (failure = error),
    heal: () => void (failure = null),
    get unreadCount() {
      return unreadCount();
    },
    publish: send,
    setStatus: (next) => {
      status = next;
      for (const f of [...followers]) f.onStatus(next);
    },
    get followers() {
      return followers.size;
    },

    follow(onChange, onStatus) {
      const follower = { onChange, onStatus };
      followers.add(follower);
      onStatus(status);
      return () => void followers.delete(follower);
    },

    async list({ unread, before, limit } = {}, signal) {
      signal?.throwIfAborted();
      if (failure) throw failure;
      const size = limit ?? DEFAULT_LIMIT;
      if (!Number.isInteger(size) || size < 1 || size > MAX_LIMIT) throw new NotificationsSourceError("INVALID_INPUT", "limit must be between 1 and 200");
      let start = 0;
      if (before) {
        start = Number(before);
        if (!Number.isInteger(start) || start < 0) throw new NotificationsSourceError("INVALID_INPUT", "before is not a cursor");
      }
      const matching = newestFirst().filter((n) => !unread || isUnread(n));
      const end = start + size;
      const page: NotificationPage = { notifications: matching.slice(start, end), unreadCount: unreadCount(), nextBefore: end < matching.length ? String(end) : "" };
      return page;
    },

    async markRead(request: ReadRequest) {
      if (failure) throw failure;
      const byIds = "ids" in request;
      if (byIds === "all" in request) throw new NotificationsSourceError("INVALID_INPUT", "send ids or all, not both or neither");
      if (byIds) {
        const missing = request.ids.find((id) => !all.some((n) => n.id === id));
        if (missing !== undefined) throw new NotificationsSourceError("NOTIFICATION_NOT_FOUND", `no notification ${missing}`);
      }
      const targets = new Set(byIds ? request.ids : all.filter((n) => n.readAt === null).map((n) => n.id));
      const changed: Notification[] = [];
      all = all.map((n) => {
        // A resolved notification that was never read gets its read_at set too.
        if (!targets.has(n.id) || n.readAt !== null) return n;
        const done = n.resolvedAt === null ? markedRead(n, source.readAt) : { ...n, readAt: source.readAt };
        changed.push(done);
        return done;
      });
      const answer: Change = { notifications: changed, unreadCount: unreadCount() };
      if (source.echo === "before") send(answer);
      await Promise.resolve();
      if (source.echo === "after") send(answer);
      return answer;
    },
  };
  return source;
}
