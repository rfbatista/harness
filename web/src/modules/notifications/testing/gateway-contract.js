// The NotificationGateway contract (../domain/ports.js), run by every
// implementation. A fake that passes it behaves like the real gateway as far
// as the bell, the panel and the inbox page can tell.

import { Codes } from "../../../shared/domain/errors.js";
import { FeedStatus } from "../../../shared/domain/feed.js";
import { flush } from "../../../shared/testing/doubles.js";
import { assert, test } from "../../../shared/testing/test.js";
import { at, makeNotification } from "./fixtures.js";

/**
 * @typedef {object} Subject
 * @property {import("../domain/ports.js").NotificationGateway} gateway
 * @property {(fields: object) => import("../domain/notification.js").Notification} notify  the server records one
 * @property {(id: string) => void} resolve  its cause is settled
 */

/**
 * @param {string} name
 * @param {(world: { notifications?: object[], beforeAnswer?: () => Promise<void> }) => Subject} makeSubject
 */
export function notificationGatewayContract(name, makeSubject) {
  const contract = (title, fn) => test(`${name} · ${title}`, fn);
  const ids = (page) => page.notifications.map((n) => n.id);

  const world = () => ({
    notifications: [
      makeNotification({ id: "old", createdAt: at(1), readAt: at(2) }),
      makeNotification({ id: "mid", createdAt: at(2), projectId: "p2", projectName: "tui", level: "info", kind: "session_failed" }),
      makeNotification({ id: "new", createdAt: at(3) }),
      makeNotification({ id: "settled", createdAt: at(0), resolvedAt: at(4) }),
    ],
  });

  contract("lists every project newest first; the count is global whatever the filter", async () => {
    const { gateway } = makeSubject(world());
    const all = await gateway.list({});
    assert.deepEqual([ids(all), all.unreadCount, all.nextBefore], [["new", "mid", "old", "settled"], 2, ""]);
    const p2 = await gateway.list({ projectId: "p2" });
    assert.deepEqual([ids(p2), p2.unreadCount], [["mid"], 2]);
    const unread = await gateway.list({ unread: true });
    assert.deepEqual(ids(unread), ["new", "mid"], "read and resolved ones are not unread");
    assert.deepEqual(ids(await gateway.list({ projectId: "p1", unread: true })), ["new"]);
  });

  contract("a notification keeps its fields; the times are Dates and the absent ones null", async () => {
    const { gateway } = makeSubject(world());
    const { notifications } = await gateway.list({});
    const n = notifications.find((x) => x.id === "mid");
    assert.deepEqual([n.projectName, n.taskTitle, n.kind, n.level, n.link], ["tui", "Notification", "session_failed", "info", "/projects/p1/tasks/t1"]);
    assert.ok(n.createdAt instanceof Date);
    assert.deepEqual([n.readAt, n.resolvedAt], [null, null]);
    assert.ok(notifications.find((x) => x.id === "settled").resolvedAt instanceof Date);
  });

  contract("pages with limit and next_before until the last page, which has none", async () => {
    const { gateway } = makeSubject(world());
    const first = await gateway.list({ limit: 3 });
    assert.deepEqual(ids(first), ["new", "mid", "old"]);
    assert.ok(first.nextBefore !== "", "a cursor while more remain");
    const second = await gateway.list({ limit: 3, before: first.nextBefore });
    assert.deepEqual([ids(second), second.nextBefore], [["settled"], ""]);
  });

  contract("a bad limit or cursor is INVALID_INPUT", async () => {
    const { gateway } = makeSubject(world());
    await assert.rejects(gateway.list({ limit: 0 }), Codes.INVALID_INPUT);
    await assert.rejects(gateway.list({ limit: 201 }), Codes.INVALID_INPUT);
    await assert.rejects(gateway.list({ before: "not-a-cursor" }), Codes.INVALID_INPUT);
  });

  contract("marking ids read answers with the ones it changed and the new count; read ones again are no error", async () => {
    const { gateway } = makeSubject(world());
    const change = await gateway.markRead({ ids: ["new"] });
    assert.deepEqual([change.notifications.map((n) => n.id), change.unreadCount], [["new"], 1]);
    assert.ok(change.notifications[0].readAt instanceof Date);
    const again = await gateway.markRead({ ids: ["new", "old"] });
    assert.deepEqual([again.notifications.length, again.unreadCount], [0, 1]);
  });

  contract("marking all read covers every project, or one", async () => {
    const one = makeSubject(world()).gateway;
    const p2 = await one.markRead({ all: true, projectId: "p2" });
    assert.deepEqual([p2.notifications.map((n) => n.id), p2.unreadCount], [["mid"], 1]);
    const all = await one.markRead({ all: true });
    assert.deepEqual([all.notifications.map((n) => n.id), all.unreadCount], [["new"], 0]);
    assert.deepEqual(ids(await one.list({ unread: true })), []);
  });

  contract("marking an unknown id is NOTIFICATION_NOT_FOUND; neither ids nor all is INVALID_INPUT", async () => {
    const { gateway } = makeSubject(world());
    await assert.rejects(gateway.markRead({ ids: ["ghost"] }), Codes.NOTIFICATION_NOT_FOUND);
    await assert.rejects(gateway.markRead({}), Codes.INVALID_INPUT);
    assert.equal((await gateway.list({})).unreadCount, 2, "a refused read changes nothing");
  });

  contract("follow reports live, streams what is created, read and resolved with the count, and stops when closed", async () => {
    const subject = makeSubject({});
    const changes = [];
    const statuses = [];
    const close = subject.gateway.follow((c) => changes.push(c), (s) => statuses.push(s));
    await flush();
    assert.ok(statuses.includes(FeedStatus.LIVE), "reports live once connected");

    const a = subject.notify({ title: "Review requested" });
    const b = subject.notify({ title: "Session waiting", projectId: "p2", kind: "session_waiting" });
    await flush();
    await subject.gateway.markRead({ ids: [a.id] });
    subject.resolve(b.id);
    await flush();
    const seen = changes.map((c) => [c.notifications.map((n) => n.title), c.unreadCount]);
    assert.deepEqual(seen, [[["Review requested"], 1], [["Session waiting"], 2], [["Review requested"], 1], [["Session waiting"], 0]]);
    assert.ok(changes[2].notifications[0].readAt instanceof Date, "the person's own read echoes");
    assert.ok(changes[3].notifications[0].resolvedAt instanceof Date);

    close();
    subject.notify({ title: "Late" });
    await flush();
    assert.equal(changes.length, 4, "nothing after close");
  });

  contract("a read is streamed before it is answered", async () => {
    let release;
    const subject = makeSubject({ notifications: [makeNotification({ id: "a" })], beforeAnswer: () => new Promise((r) => (release = r)) });
    const changes = [];
    const close = subject.gateway.follow((c) => changes.push(c), () => {});
    await flush();
    let answered = false;
    const pending = subject.gateway.markRead({ ids: ["a"] }).then((c) => ((answered = true), c));
    await flush();
    assert.deepEqual([changes.length, answered], [1, false], "the echo arrives while the POST is in flight");
    release();
    const change = await pending;
    assert.equal(change.unreadCount, 0);
    close();
  });
}
