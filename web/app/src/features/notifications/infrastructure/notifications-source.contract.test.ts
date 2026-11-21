import { test } from "node:test";
import assert from "node:assert/strict";
import { apiClient } from "../../../shared/infrastructure/api-client";
import { memoryFeed } from "../../../shared/infrastructure/memory-feed";
import { NotificationsSourceError } from "../application/ports";
import { fakeNotificationsServer } from "../testing/fake-server";
import { notificationsSourceContract } from "../testing/notifications-source-contract";
import { toWire } from "../testing/wire";
import { httpNotificationsSource, NOTIFICATIONS_STREAM } from "./http-notifications-source";
import { memoryNotificationsSource } from "./memory-notifications-source";

notificationsSourceContract("HTTP adapter against a fake server on the wire", () => {
  const data = memoryNotificationsSource();
  const server = fakeNotificationsServer(data);
  const feed = memoryFeed();
  return {
    source: httpNotificationsSource(apiClient(server.fetch), feed),
    seed: data.seed,
    breakNetwork: server.breakNetwork,
    publish: (change) => feed.emit(NOTIFICATIONS_STREAM, { changed: change.notifications.map(toWire), unread_count: change.unreadCount }),
    setStatus: (status) => feed.setStatus(NOTIFICATIONS_STREAM, status),
  };
});

notificationsSourceContract("in-memory adapter", () => {
  const source = memoryNotificationsSource();
  return { source, seed: source.seed, breakNetwork: () => source.failWith(new NotificationsSourceError("NETWORK_ERROR", "offline")), publish: source.publish, setStatus: source.setStatus };
});

test("the HTTP adapter follows the notifications stream, and drops a message it cannot read", () => {
  const feed = memoryFeed();
  const source = httpNotificationsSource(apiClient(fakeNotificationsServer(memoryNotificationsSource()).fetch), feed);
  const counts: number[] = [];
  const stop = source.follow((c) => counts.push(c.unreadCount), () => {});
  assert.equal(feed.followers(NOTIFICATIONS_STREAM), 1);
  feed.emit(NOTIFICATIONS_STREAM, { changed: [{ id: "" }], unread_count: 1 });
  feed.emit(NOTIFICATIONS_STREAM, "garbage");
  feed.emit(NOTIFICATIONS_STREAM, { changed: [], unread_count: 2 });
  assert.deepEqual(counts, [2]);
  stop();
  assert.equal(feed.followers(NOTIFICATIONS_STREAM), 0);
});
