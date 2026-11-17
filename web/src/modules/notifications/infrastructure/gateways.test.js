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

test("the branch gate's and the services' notifications map", () => {
  const dto = notificationDTO(makeNotification());
  for (const kind of ["branch_request", "branch_finding", "service_request"]) {
    const n = toNotification({ ...dto, kind, level: "action", link: "/projects/p1/tasks/t1?session=s1#branch-request-r1" });
    assert.equal(n.kind, kind);
  }
});

test("a kind this client does not know reads as a generic notification and leaves the rest of the list whole", () => {
  const dto = notificationDTO(makeNotification());
  const n = toNotification({ ...dto, kind: "branch_gossip", level: "action", title: "Something new", body: "Details", link: "/projects/p1" });
  assert.deepEqual([n.kind, n.level, n.title, n.body, n.link], ["agent", "action", "Something new", "Details", "/projects/p1"]);
  const page = toPage({ notifications: [dto, { ...dto, id: "n2", kind: "gossip" }, { ...dto, id: "n3", kind: "service_request" }], unread_count: 3 });
  assert.deepEqual(page.notifications.map((x) => x.kind), [dto.kind, "agent", "service_request"]);
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

test("revision 6 fields map: the actor, the verb and object, the repository, the outcome and the listed sessions", () => {
  const dto = notificationDTO(makeNotification());
  const n = toNotification({
    ...dto, kind: "commit_check_off", level: "action", resolved_at: "2026-10-09T15:00:00Z", outcome: "repaired",
    actor: { kind: "system", name: "harness" }, verb: "could not guard 2 sessions", object: { text: "commit checks off", code: false }, repository_name: "api",
    listed_sessions: [{ session_id: "s1", task_id: "t1", task_title: "Ship", agent_name: "go-developer", link: "/projects/p1/tasks/t1?session=s1", state: "off" }, { session_id: "s2", agent_name: "devops", link: "/projects/p1?session=s2", state: "ended" }],
  });
  assert.deepEqual([n.actor.kind, n.actor.name, n.actor.mode, n.verb, n.object.text, n.object.code, n.repositoryName, n.outcome], ["system", "harness", "", "could not guard 2 sessions", "commit checks off", false, "api", "repaired"]);
  assert.deepEqual(n.listedSessions.map((s) => [s.sessionId, s.taskTitle, s.agentName, s.state]), [["s1", "Ship", "go-developer", "off"], ["s2", "", "devops", "ended"]]);
  const agent = toNotification({ ...dto, actor: { kind: "agent", name: "devops", mode: "operate", session_id: "s9" } });
  assert.deepEqual([agent.actor.kind, agent.actor.mode, agent.actor.sessionId], ["agent", "operate", "s9"]);
});

test("revision 5 data has no actor, and unknown actor kinds and outcomes do not take the list down", () => {
  const dto = notificationDTO(makeNotification());
  const old = toNotification(dto);
  assert.deepEqual([old.actor.name, old.actor.kind, old.verb, old.object, old.repositoryName, old.outcome, old.listedSessions], ["harness", "system", "", null, "", "", []]);
  const odd = toNotification({ ...dto, actor: { kind: "robot", name: "x" }, outcome: "vaporised" });
  assert.deepEqual([odd.actor.kind, odd.actor.name, odd.outcome], ["system", "x", ""]);
  assert.throws(() => toNotification({ ...dto, listed_sessions: [{ session_id: "s", link: "https://x", state: "off" }] }), Codes.BAD_RESPONSE);
});

test("open_count maps on a page, a read answer and a stream message, and is null when the server sends none", () => {
  assert.equal(toPage({ notifications: [], unread_count: 3, open_count: 2 }).openCount, 2);
  assert.equal(toPage({ notifications: [], unread_count: 3 }).openCount, null);
  assert.equal(toReadAnswer({ notifications: [], unread_count: 3, open_count: 0 }).openCount, 0);
  assert.equal(toStreamChange({ changed: [], unread_count: 1, open_count: 4 }).openCount, 4);
  assert.equal(toStreamChange({ changed: [], unread_count: 1 }).openCount, null);
  assert.throws(() => toPage({ notifications: [], unread_count: 1, open_count: -1 }), Codes.BAD_RESPONSE);
});
