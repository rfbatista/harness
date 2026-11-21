// The contract every NotificationsSource adapter keeps (Contract: Harness server
// ↔ Web UI — notifications). Run against the HTTP and in-memory adapters.

import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { NotificationsSourceError, type NotificationsSource } from "../application/ports";
import type { FeedStatus } from "../domain/feed-status";
import type { Change, Notification } from "../domain/notification";
import { notification, read } from "./fixtures";

export interface NotificationsSourceHarness {
  source: NotificationsSource;
  /** The server holds these notifications. */
  seed(notifications: readonly Notification[]): void;
  /** Every next call fails as the network does. */
  breakNetwork(): void;
  /** The server's stream sends `change`. */
  publish(change: Change): void;
  /** The stream's state changes. */
  setStatus(status: FeedStatus): void;
}

const rejectsWith = (code: string) => (err: unknown) => err instanceof NotificationsSourceError && err.code === code;

export function notificationsSourceContract(name: string, make: () => NotificationsSourceHarness): void {
  describe(`NotificationsSource contract: ${name}`, () => {
    test("lists newest first, with the global unread count", async () => {
      const h = make();
      const [newest, middle, oldest] = [notification(), notification(), notification()] as const;
      h.seed([oldest, newest, read(middle)]);
      const page = await h.source.list({});
      assert.deepEqual(page.notifications, [newest, read(middle), oldest].sort((a, b) => b.createdAt.getTime() - a.createdAt.getTime()));
      assert.equal(page.unreadCount, 2);
      assert.equal(page.nextBefore, "");
    });

    test("unread lists only the unread ones, and the count stays global", async () => {
      const h = make();
      const [a, b, c] = [notification(), notification(), notification()] as const;
      h.seed([a, read(b), { ...c, resolvedAt: new Date(Date.UTC(2026, 10, 21, 12, 30)) }]);
      const page = await h.source.list({ unread: true });
      assert.deepEqual(page.notifications.map((n) => n.id), [a.id]);
      assert.equal(page.unreadCount, 1);
    });

    test("pages with an opaque cursor, without overlap, to the end", async () => {
      const h = make();
      const all = [notification(), notification(), notification(), notification(), notification()];
      h.seed(all);
      const first = await h.source.list({ limit: 2 });
      assert.deepEqual(first.notifications.map((n) => n.id), [all[0]!.id, all[1]!.id]);
      assert.notEqual(first.nextBefore, "");
      const second = await h.source.list({ limit: 2, before: first.nextBefore });
      assert.deepEqual(second.notifications.map((n) => n.id), [all[2]!.id, all[3]!.id]);
      const last = await h.source.list({ limit: 2, before: second.nextBefore });
      assert.deepEqual(last.notifications.map((n) => n.id), [all[4]!.id]);
      assert.equal(last.nextBefore, "");
    });

    test("a bad limit or cursor is INVALID_INPUT", async () => {
      const h = make();
      await assert.rejects(h.source.list({ limit: 0 }), rejectsWith("INVALID_INPUT"));
      await assert.rejects(h.source.list({ limit: 201 }), rejectsWith("INVALID_INPUT"));
      await assert.rejects(h.source.list({ before: "garbage" }), rejectsWith("INVALID_INPUT"));
    });

    test("a network failure is NETWORK_ERROR", async () => {
      const h = make();
      h.breakNetwork();
      await assert.rejects(h.source.list({}), rejectsWith("NETWORK_ERROR"));
    });

    test("an aborted call rejects with AbortError", async () => {
      const controller = new AbortController();
      controller.abort();
      await assert.rejects(make().source.list({}, controller.signal), (err: unknown) => err instanceof Error && err.name === "AbortError");
    });

    test("follow delivers each change with the count as of it, in order, and the stream's status", () => {
      const h = make();
      const changes: Change[] = [];
      const statuses: FeedStatus[] = [];
      h.source.follow((c) => changes.push(c), (s) => statuses.push(s));
      const first = notification();
      const second = read(first);
      h.setStatus("live");
      h.publish({ notifications: [first], unreadCount: 1 });
      h.publish({ notifications: [second], unreadCount: 0 });
      assert.deepEqual(changes, [{ notifications: [first], unreadCount: 1 }, { notifications: [second], unreadCount: 0 }]);
      assert.equal(statuses.at(-1), "live");
      assert.equal(statuses[0], "connecting");
    });

    test("follow carries an empty change (a deleted project took its notifications along)", () => {
      const h = make();
      const changes: Change[] = [];
      h.source.follow((c) => changes.push(c), () => {});
      h.publish({ notifications: [], unreadCount: 4 });
      assert.deepEqual(changes, [{ notifications: [], unreadCount: 4 }]);
    });

    test("follow reports a drop and the resync that follows it", () => {
      const h = make();
      const statuses: FeedStatus[] = [];
      h.source.follow(() => {}, (s) => statuses.push(s));
      h.setStatus("live");
      h.setStatus("paused");
      h.setStatus("resynced");
      assert.deepEqual(statuses.slice(-3), ["live", "paused", "resynced"]);
    });

    test("follow delivers nothing after it is stopped", () => {
      const h = make();
      const changes: Change[] = [];
      const stop = h.source.follow((c) => changes.push(c), () => {});
      stop();
      h.publish({ notifications: [], unreadCount: 1 });
      assert.deepEqual(changes, []);
    });
  });
}
