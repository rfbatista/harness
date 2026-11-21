// NotificationsSource in memory, for tests and for development without a
// server. It answers like the server: newest first, the global count whatever
// the filter, an opaque cursor, INVALID_INPUT for a bad limit.

import { NotificationsSourceError, type NotificationsSource } from "../application/ports";
import type { FeedStatus } from "../domain/feed-status";
import { isUnread, type Change, type Notification, type NotificationPage } from "../domain/notification";

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
}

export function memoryNotificationsSource(): MemoryNotificationsSource {
  let all: Notification[] = [];
  let failure: NotificationsSourceError | null = null;
  const followers = new Set<{ onChange: (change: Change) => void; onStatus: (status: FeedStatus) => void }>();
  let status: FeedStatus = "connecting";
  const newestFirst = () => [...all].sort((a, b) => b.createdAt.getTime() - a.createdAt.getTime() || (a.id < b.id ? 1 : -1));
  return {
    seed: (notifications) => void (all = [...notifications]),
    failWith: (error) => void (failure = error),
    heal: () => void (failure = null),
    get unreadCount() {
      return all.filter(isUnread).length;
    },
    publish: (change) => {
      for (const f of [...followers]) f.onChange(change);
    },
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
      const page: NotificationPage = { notifications: matching.slice(start, end), unreadCount: all.filter(isUnread).length, nextBefore: end < matching.length ? String(end) : "" };
      return page;
    },
  };
}
