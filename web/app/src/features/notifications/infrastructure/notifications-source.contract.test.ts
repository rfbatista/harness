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
    publish: (change) => feed.emit(NOTIFICATIONS_STREAM, { changed: change.notifications.map(toWire), unread_count: change.unreadCount, ...(change.openCount == null ? {} : { open_count: change.openCount }) }),
    setStatus: (status) => feed.setStatus(NOTIFICATIONS_STREAM, status),
    answerMalformed: server.answerWith,
    answerWith: server.answerWith,
    publishMalformed: (raw) => feed.emit(NOTIFICATIONS_STREAM, raw),
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

test("the HTTP adapter reads an outcome or an actor kind it does not know leniently", async () => {
  const data = memoryNotificationsSource();
  const server = fakeNotificationsServer(data);
  server.answerWith({
    notifications: [{ id: "n1", project_id: "p", project_name: "harness", source: "system", kind: "task_review", level: "action", title: "T", link: "/x", created_at: "2026-11-21T12:00:00Z", outcome: "levitated", actor: { kind: "robot", name: "devops" } }],
    unread_count: 1,
  });
  const page = await httpNotificationsSource(apiClient(server.fetch), memoryFeed()).list({});
  const [n] = page.notifications;
  assert.deepEqual([n?.kind, n?.outcome, n?.actor.kind, n?.actor.name], ["task_review", "", "system", "devops"]);
  assert.equal(page.openCount, null, "a server that sends no open_count");
});

test("the HTTP adapter drops a malformed session entry and keeps the notification, and reads a malformed actor as the harness", async () => {
  const data = memoryNotificationsSource();
  const server = fakeNotificationsServer(data);
  server.answerWith({
    notifications: [
      {
        id: "n1", project_id: "p", project_name: "harness", source: "system", kind: "sessions_interrupted", level: "action", title: "T", link: "/x", created_at: "2026-11-21T12:00:00Z",
        actor: { kind: "agent" },
        interrupted_sessions: [{ session_id: "s1", link: "/s1", state: "failed" }, { session_id: "s2", link: "//evil", state: "failed" }, { session_id: "s3", link: "/s3", state: "lost" }],
        listed_sessions: [{ session_id: "l1", link: "/l1", state: "off" }, { link: "/l2" }],
      },
    ],
    unread_count: 1,
    open_count: 1,
  });
  const page = await httpNotificationsSource(apiClient(server.fetch), memoryFeed()).list({});
  const [n] = page.notifications;
  assert.deepEqual([n?.interruptedSessions.map((s) => s.sessionId), n?.listedSessions.map((s) => s.sessionId), n?.actor.name, page.openCount], [["s1"], ["l1"], "harness", 1]);
});
