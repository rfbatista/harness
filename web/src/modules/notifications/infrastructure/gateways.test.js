// Both NotificationGateway implementations run the same contract.

import { Codes } from "../../../shared/domain/errors.js";
import { FeedStatus } from "../../../shared/domain/feed.js";
import { apiClient } from "../../../shared/infrastructure/api.js";
import { feed } from "../../../shared/infrastructure/feed.js";
import { fakeFetch, flush, jsonResponse } from "../../../shared/testing/doubles.js";
import { assert, file, test } from "../../../shared/testing/test.js";
import { notificationGatewayContract } from "../testing/gateway-contract.js";
import { makeNotification, notificationDTO } from "../testing/fixtures.js";
import { stubNotificationsApi } from "../testing/stub-api.js";
import { toNotification, toPage, toReadAnswer, toStreamChange } from "./dto.js";
import { memoryNotifications } from "./memory-gateway.js";
import { notificationsGateway } from "./notifications-gateway.js";

file("notifications/infrastructure/gateways");

notificationGatewayContract("memory", (world) => {
  const memory = memoryNotifications(world);
  return { gateway: memory.gateway, notify: memory.notify, resolve: memory.resolve };
});

test("memory · the stream's state is the test's to report", async () => {
  const memory = memoryNotifications();
  const heard = [];
  const stop = memory.gateway.follow(() => {}, (s) => heard.push(s));
  await flush();
  memory.reportStatus(FeedStatus.PAUSED);
  memory.reportStatus(FeedStatus.RESYNCED);
  stop();
  memory.reportStatus(FeedStatus.PAUSED);
  assert.deepEqual(heard, [FeedStatus.LIVE, FeedStatus.PAUSED, FeedStatus.RESYNCED]);
});

function http(world) {
  const stub = stubNotificationsApi(world);
  const gateway = notificationsGateway(apiClient({ base: "/api", fetch: stub.fetch }), feed({ base: "/api", EventSource: stub.EventSource }));
  return { gateway, stub };
}

notificationGatewayContract("http", (world) => {
  const { gateway, stub } = http(world);
  return { gateway, notify: stub.memory.notify, resolve: stub.memory.resolve };
});

test("http · the query and the bodies are the contract's", async () => {
  const { fetch, calls } = fakeFetch(() => jsonResponse(200, { notifications: [], unread_count: 0 }));
  const gateway = notificationsGateway(apiClient({ base: "/api", fetch }), feed({ base: "/api", EventSource: class {} }));
  await gateway.list({ projectId: "p 1", unread: true, before: "c1", limit: 20 });
  await gateway.list({});
  await gateway.markRead({ ids: ["a", "b"] });
  await gateway.markRead({ all: true, projectId: "p1" });
  await gateway.markRead({ all: true });
  assert.deepEqual(
    calls.map((c) => [c.init.method, c.url, c.init.body ?? null]),
    [
      ["GET", "/api/notifications?project_id=p+1&unread=true&before=c1&limit=20", null],
      ["GET", "/api/notifications", null],
      ["POST", "/api/notifications/read", '{"ids":["a","b"]}'],
      ["POST", "/api/notifications/read", '{"all":true,"project_id":"p1"}'],
      ["POST", "/api/notifications/read", '{"all":true}'],
    ],
  );
});

test("http · a malformed stream message is dropped, the stream stays open", async () => {
  const { gateway, stub } = http({});
  const changes = [];
  const close = gateway.follow((c) => changes.push(c), () => {});
  await flush();
  stub.pushRaw({ changed: [{ id: "" }], unread_count: 1 });
  stub.pushRaw({ ticket: { id: "t1" } });
  stub.pushRaw("not json");
  stub.memory.notify({ title: "After" });
  await flush();
  assert.deepEqual(changes.map((c) => c.notifications[0].title), ["After"]);
  close();
});

test("a notification without its optional fields reads them as empty", () => {
  const n = toNotification({ id: "n1", project_id: "p1", source: "agent", kind: "agent", level: "info", title: "Built", link: "/projects/p1", created_at: "2026-10-09T14:00:00Z" });
  assert.deepEqual([n.taskId, n.taskTitle, n.sessionId, n.body, n.readAt, n.resolvedAt], ["", "", "", "", null, null]);
  assert.ok(Object.isFrozen(n));
});

test("the branch gate's notifications map; any other unknown kind is still BAD_RESPONSE", () => {
  const dto = notificationDTO(makeNotification());
  for (const kind of ["branch_request", "branch_finding"]) {
    const n = toNotification({ ...dto, kind, level: "action", link: "/projects/p1/tasks/t1?session=s1#branch-request-r1" });
    assert.equal(n.kind, kind);
  }
  assert.throws(() => toNotification({ ...dto, kind: "branch_gossip" }), Codes.BAD_RESPONSE);
});

test("a sessions_interrupted notification maps its list, whose entries default their optional fields", () => {
  const dto = notificationDTO(makeNotification());
  const n = toNotification({
    ...dto, kind: "sessions_interrupted", level: "action", link: "/notifications",
    interrupted_sessions: [
      { session_id: "s1", project_id: "p1", task_id: "t1", task_title: "Ship", agent_name: "go-developer", link: "/projects/p1/tasks/t1?session=s1", state: "failed", reason: "worktree gone" },
      { session_id: "s2", project_id: "p1", agent_name: "devops", link: "/projects/p1?session=s2", state: "interrupted" },
    ],
  });
  assert.equal(n.kind, "sessions_interrupted");
  assert.deepEqual(n.interruptedSessions.map((s) => [s.sessionId, s.state, s.reason, s.taskTitle]), [["s1", "failed", "worktree gone", "Ship"], ["s2", "interrupted", "", ""]]);
  assert.deepEqual(toNotification(dto).interruptedSessions, []);
  assert.throws(() => toNotification({ ...dto, interrupted_sessions: [{ session_id: "s1", link: "/p", state: "lost" }] }), Codes.BAD_RESPONSE);
  assert.throws(() => toNotification({ ...dto, interrupted_sessions: [{ session_id: "s1", link: "https://x", state: "gone" }] }), Codes.BAD_RESPONSE);
});

test("a malformed notification or answer is BAD_RESPONSE; a link off the web UI is refused", () => {
  const dto = notificationDTO(makeNotification());
  assert.throws(() => toNotification({ ...dto, id: "" }), Codes.BAD_RESPONSE);
  assert.throws(() => toNotification({ ...dto, kind: "gossip" }), Codes.BAD_RESPONSE);
  assert.throws(() => toNotification({ ...dto, level: "urgent" }), Codes.BAD_RESPONSE);
  assert.throws(() => toNotification({ ...dto, source: "person" }), Codes.BAD_RESPONSE);
  assert.throws(() => toNotification({ ...dto, created_at: "" }), Codes.BAD_RESPONSE);
  assert.throws(() => toNotification({ ...dto, read_at: "yesterday" }), Codes.BAD_RESPONSE);
  for (const link of ["https://evil.test/", "//evil.test/", "javascript:alert(1)", "projects/p1", undefined]) {
    assert.throws(() => toNotification({ ...dto, link }), Codes.BAD_RESPONSE);
  }
  assert.throws(() => toPage({ unread_count: 0 }), Codes.BAD_RESPONSE);
  assert.throws(() => toPage({ notifications: [], unread_count: -1 }), Codes.BAD_RESPONSE);
  assert.throws(() => toReadAnswer({ notifications: [] }), Codes.BAD_RESPONSE);
  assert.throws(() => toStreamChange({ notifications: [], unread_count: 0 }), Codes.BAD_RESPONSE); // the stream says changed
  assert.equal(toPage({ notifications: [], unread_count: 3 }).nextBefore, "");
});
