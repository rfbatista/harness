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
  /** The server answers `raw` (not valid) to every request. Adapters that read no wire have none. */
  answerMalformed?(raw: unknown): void;
  /** The server answers `raw` (well formed) to every request. Adapters that read no wire have none. */
  answerWith?(raw: unknown): void;
  /** The stream sends `raw` (not a valid change event). */
  publishMalformed?(raw: unknown): void;
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

    test("the open count is global: level action and not settled, whatever the filter, and reading does not change it", async () => {
      const h = make();
      const [open, info, settled] = [notification(), notification({ level: "info" }), notification({ resolvedAt: new Date(Date.UTC(2026, 10, 21, 12, 30)) })] as const;
      h.seed([open, info, settled]);
      assert.equal((await h.source.list({})).openCount, 1);
      assert.equal((await h.source.list({ unread: true })).openCount, 1);
      assert.equal((await h.source.markRead({ ids: [open.id] })).openCount, 1, "read is not settled");
    });

    test("open lists only the open causes, however old, and combines with unread", async () => {
      const h = make();
      const older = notification();
      const infos = [notification({ level: "info" }), notification({ level: "info" })];
      h.seed([older, ...infos, read(notification())]);
      const page = await h.source.list({ open: true });
      assert.deepEqual(page.notifications.map((n) => n.id).sort(), [older.id, page.notifications.find((n) => n.readAt !== null)!.id].sort());
      assert.deepEqual((await h.source.list({ open: true, unread: true })).notifications.map((n) => n.id), [older.id]);
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

    test("marking ids read answers the notifications it changed and the global count, and skips ids already read", async () => {
      const h = make();
      const [a, b, c] = [notification(), notification(), notification()] as const;
      h.seed([a, b, read(c)]);
      const answer = await h.source.markRead({ ids: [a.id, c.id] });
      assert.deepEqual(answer.notifications.map((n) => n.id), [a.id]);
      assert.notEqual(answer.notifications[0]!.readAt, null);
      assert.equal(answer.unreadCount, 1);
      const page = await h.source.list({ unread: true });
      assert.deepEqual(page.notifications.map((n) => n.id), [b.id]);
    });

    test("marking all read changes every unread one, and a resolved one that was never read gets its read time too", async () => {
      const h = make();
      const [a, b] = [notification(), notification()] as const;
      const resolved = { ...notification(), resolvedAt: new Date(Date.UTC(2026, 10, 21, 12, 30)) };
      h.seed([a, b, resolved]);
      const answer = await h.source.markRead({ all: true });
      assert.deepEqual(answer.notifications.map((n) => n.id).sort(), [a.id, b.id, resolved.id].sort());
      assert.equal(answer.notifications.every((n) => n.readAt !== null), true);
      assert.equal(answer.unreadCount, 0);
      assert.equal((await h.source.list({ unread: true })).notifications.length, 0);
    });

    test("an unknown id fails the whole call with NOTIFICATION_NOT_FOUND and marks nothing", async () => {
      const h = make();
      const a = notification();
      h.seed([a]);
      await assert.rejects(h.source.markRead({ ids: [a.id, "nope"] }), rejectsWith("NOTIFICATION_NOT_FOUND"));
      assert.equal((await h.source.list({})).unreadCount, 1);
    });

    test("neither ids nor all is INVALID_INPUT", async () => {
      await assert.rejects(make().source.markRead({} as never), rejectsWith("INVALID_INPUT"));
    });

    test("a network failure while marking is NETWORK_ERROR", async () => {
      const h = make();
      h.breakNetwork();
      await assert.rejects(h.source.markRead({ all: true }), rejectsWith("NETWORK_ERROR"));
    });

    const wireNotification = { id: "n1", project_id: "p1", project_name: "harness", source: "system", kind: "task_review", level: "action", title: "T", link: "/x", created_at: "2026-11-21T12:00:00Z" };
    const MALFORMED: [string, unknown][] = [
      ["a page without its count", { notifications: [] }],
      ["a count that is not a number", { notifications: [], unread_count: "3" }],
      ["a notification list that is not a list", { notifications: {}, unread_count: 1 }],
      ["a notification without an id", { notifications: [{ ...wireNotification, id: undefined }], unread_count: 1 }],
      ["an open_count that is not a count", { notifications: [], unread_count: 1, open_count: "2" }],
      ["a notification with a link off the web UI", { notifications: [{ ...wireNotification, link: "//evil.example/x" }], unread_count: 1 }],
      ["a notification with a time that is not a time", { notifications: [{ ...wireNotification, created_at: "yesterday" }], unread_count: 1 }],
      ["not an object", [1, 2]],
    ];
    for (const [what, raw] of MALFORMED) {
      test(`${what} in a list or a mark-read answer is BAD_RESPONSE`, async (t) => {
        const h = make();
        if (!h.answerMalformed) return t.skip("the adapter reads no wire");
        h.answerMalformed(raw);
        await assert.rejects(h.source.list({}), rejectsWith("BAD_RESPONSE"));
        await assert.rejects(h.source.markRead({ all: true }), rejectsWith("BAD_RESPONSE"));
      });
    }

    const known = { ...wireNotification, id: "known" };
    const futureKind = { ...wireNotification, id: "future", kind: "from_the_future" };

    test("a notification of a kind the client does not know is dropped from a list and from a mark-read answer: its own row, never the page", async (t) => {
      const h = make();
      if (!h.answerWith) return t.skip("the adapter reads no wire");
      h.answerWith({ notifications: [known, futureKind], unread_count: 2, open_count: 2 });
      const page = await h.source.list({});
      assert.deepEqual([page.notifications.map((n) => n.id), page.unreadCount, page.openCount], [["known"], 2, 2], "the counts stay the server's");
      const answer = await h.source.markRead({ all: true });
      assert.deepEqual(answer.notifications.map((n) => n.id), ["known"]);
    });

    test("a change event that carries a notification of an unknown kind delivers the rest, with its counts", (t) => {
      const h = make();
      if (!h.publishMalformed) return t.skip("the adapter reads no wire");
      const changes: Change[] = [];
      h.source.follow((c) => changes.push(c), () => {});
      h.publishMalformed({ changed: [known, futureKind], unread_count: 2, open_count: 2 });
      assert.deepEqual(changes.map((c) => [c.notifications.map((n) => n.id), c.unreadCount, c.openCount]), [[["known"], 2, 2]]);
    });

    for (const [what, raw] of [["without its count", { changed: [] }], ["whose changed is not a list", { changed: 3, unread_count: 1 }], ["with an unreadable notification", { changed: [{ id: 5 }], unread_count: 1 }], ["that is a string", "garbage"]] as [string, unknown][]) {
      test(`a change event ${what} is dropped, and the next valid one is delivered`, (t) => {
        const h = make();
        if (!h.publishMalformed) return t.skip("the adapter reads no wire");
        const counts: number[] = [];
        h.source.follow((c) => counts.push(c.unreadCount), () => {});
        h.publishMalformed(raw);
        h.publish({ notifications: [], unreadCount: 7 });
        assert.deepEqual(counts, [7]);
      });
    }

    test("follow delivers each change with the count as of it, in order, and the stream's status", () => {
      const h = make();
      const changes: Change[] = [];
      const statuses: FeedStatus[] = [];
      h.source.follow((c) => changes.push(c), (s) => statuses.push(s));
      const first = notification();
      const second = read(first);
      h.setStatus("live");
      h.publish({ notifications: [first], unreadCount: 1, openCount: 1 });
      h.publish({ notifications: [second], unreadCount: 0, openCount: 1 });
      assert.deepEqual(changes, [{ notifications: [first], unreadCount: 1, openCount: 1 }, { notifications: [second], unreadCount: 0, openCount: 1 }]);
      assert.equal(statuses.at(-1), "live");
      assert.equal(statuses[0], "connecting");
    });

    test("follow carries an empty change (a deleted project took its notifications along)", () => {
      const h = make();
      const changes: Change[] = [];
      h.source.follow((c) => changes.push(c), () => {});
      h.publish({ notifications: [], unreadCount: 4, openCount: 3 });
      assert.deepEqual(changes, [{ notifications: [], unreadCount: 4, openCount: 3 }]);
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
