import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { notification, read } from "../testing/fixtures";
import { memoryNavigator } from "../infrastructure/memory-navigator";
import { memoryNotificationsSource } from "../infrastructure/memory-notifications-source";
import { Inbox } from "./inbox";

const settle = () => new Promise((resolve) => setImmediate(resolve));
const page = (unreadCount: number) => ({ notifications: [], unreadCount, nextBefore: "" });

describe("the inbox follows the stream", () => {
  test("an arrival, a read and a resolution each set the count to the stream's", () => {
    const source = memoryNotificationsSource();
    const inbox = new Inbox(source, { navigator: memoryNavigator() });
    inbox.start(page(2));
    const n = notification();
    source.publish({ notifications: [n], unreadCount: 3 });
    assert.equal(inbox.state.unreadCount, 3);
    source.publish({ notifications: [read(n)], unreadCount: 2 });
    assert.equal(inbox.state.unreadCount, 2);
    source.publish({ notifications: [{ ...n, resolvedAt: new Date() }], unreadCount: 1 });
    assert.equal(inbox.state.unreadCount, 1);
  });

  test("the same message twice leaves the same count", () => {
    const source = memoryNotificationsSource();
    const inbox = new Inbox(source, { navigator: memoryNavigator() });
    inbox.start(page(0));
    const change = { notifications: [notification()], unreadCount: 1 };
    source.publish(change);
    source.publish(change);
    assert.equal(inbox.state.unreadCount, 1);
  });

  test("a stream message that arrives before the first read answers wins over it", async () => {
    const source = memoryNotificationsSource();
    source.seed([notification()]);
    const inbox = new Inbox(source, { navigator: memoryNavigator() });
    inbox.start(null);
    source.publish({ notifications: [], unreadCount: 9 });
    await settle();
    assert.equal(inbox.state.unreadCount, 9);
  });

  test("shows the stream's state: down is paused, up clears it", () => {
    const source = memoryNotificationsSource();
    const inbox = new Inbox(source, { navigator: memoryNavigator() });
    inbox.start(page(1));
    assert.equal(inbox.state.feed, "connecting");
    source.setStatus("live");
    assert.equal(inbox.state.feed, "live");
    source.setStatus("paused");
    assert.equal(inbox.state.feed, "paused");
    source.setStatus("resynced");
    source.setStatus("live");
    assert.equal(inbox.state.feed, "live");
  });

  test("after a resync with no view watching, the count is read again", async () => {
    const source = memoryNotificationsSource();
    source.seed([notification(), notification(), notification()]);
    const inbox = new Inbox(source, { navigator: memoryNavigator() });
    inbox.start(page(1));
    source.setStatus("paused");
    source.setStatus("resynced");
    await settle();
    assert.equal(inbox.state.unreadCount, 3);
  });

  test("a watching view gets the changes, and a reload on an empty change or a resync", () => {
    const source = memoryNotificationsSource();
    const inbox = new Inbox(source, { navigator: memoryNavigator() });
    inbox.start(page(0));
    const applied: string[][] = [];
    let reloads = 0;
    const unwatch = inbox.watch({ apply: (changed) => void applied.push(changed.map((n) => n.id)), readAll: () => () => {}, reload: () => void reloads++ });
    const n = notification();
    source.publish({ notifications: [n], unreadCount: 1 });
    source.publish({ notifications: [], unreadCount: 0 });
    source.setStatus("resynced");
    assert.deepEqual(applied, [[n.id]]);
    assert.equal(reloads, 2);
    unwatch();
    source.publish({ notifications: [n], unreadCount: 1 });
    assert.equal(applied.length, 1);
  });

  test("stops following when stopped", () => {
    const source = memoryNotificationsSource();
    const inbox = new Inbox(source, { navigator: memoryNavigator() });
    inbox.start(page(0));
    assert.equal(source.followers, 1);
    inbox.stop();
    assert.equal(source.followers, 0);
  });
});
