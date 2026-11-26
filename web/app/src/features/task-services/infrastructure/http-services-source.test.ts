import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { apiClient, type FetchLike } from "../../../shared/infrastructure/api-client";
import { memoryFeed } from "../../../shared/infrastructure/memory-feed";
import { ServicesSourceError, type ServicesEvent } from "../application/ports";
import { httpServicesSource } from "./http-services-source";

const wire = {
  id: "s1", name: "api", kind: "command", status: "exited", unknown_extra: 1,
  command: { line: "go run .", ports: [8080] },
  run: { started_at: "2026-11-25T10:00:00Z", ended_at: "2026-11-25T10:05:00Z", exit_code: 2, error: "boom", started_by: { kind: "session", task_id: "t1", agent: "claude" } },
  created_by: { kind: "session", task_id: "t1", agent: "claude" },
  credentials: [{ label: "user", value: "ana" }, { nope: 1 }],
};

function recording(answer: unknown = {}) {
  const calls: { url: string; method?: string; body?: unknown }[] = [];
  const fetcher = (async (url: string, init?: RequestInit) => (calls.push({ url, method: init?.method, body: init?.body ? JSON.parse(init.body as string) : undefined }), Response.json(answer))) as FetchLike;
  return { calls, feed: memoryFeed(), source: (feed = memoryFeed()) => httpServicesSource(apiClient(fetcher), feed) };
}

describe("HTTP services source", () => {
  test("lists the task's services with task_id, mapping run, creator and credentials; an unreadable credential is dropped", async () => {
    const r = recording({ services: [wire], seq: 7 });
    const list = await r.source().list("p1", "t1");
    assert.deepEqual(r.calls.map((c) => c.url), ["/api/list_services?project_id=p1&task_id=t1"]);
    assert.equal(list.seq, 7);
    const s = list.services[0]!;
    assert.equal(s.run?.exitCode, 2);
    assert.equal(s.run?.startedBy.taskId, "t1");
    assert.deepEqual(s.credentials, [{ label: "user", value: "ana" }]);
  });

  test("a service list that does not read is BAD_RESPONSE", async () => {
    await assert.rejects(recording({ nope: 1 }).source().list("p1", "t1"), (e: unknown) => e instanceof ServicesSourceError && e.code === "BAD_RESPONSE");
    await assert.rejects(recording({ services: [{ ...wire, status: "bogus" }] }).source().list("p1", "t1"), (e: unknown) => e instanceof ServicesSourceError && e.code === "BAD_RESPONSE");
  });

  test("asks for the task's pending requests only", async () => {
    const r = recording({ requests: [{ id: "q1", action: "restart", state: "pending", task_id: "t1", service_name: "api", credentials: [{ label: "x", value: "secret", change: "set" }] }] });
    const [q] = await r.source().requests("p1", "t1");
    assert.deepEqual(r.calls.map((c) => c.url), ["/api/service_requests?project_id=p1&task_id=t1&state=pending"]);
    assert.equal(q?.serviceName, "api");
    assert.equal(JSON.stringify(q).includes("secret"), false, "a request's credential values are never read");
  });

  test("start, stop and restart post the service and the task", async () => {
    const r = recording({ service: wire });
    const source = r.source();
    await source.act("stop", "s1", "t1");
    await source.act("restart", "s1", "t1");
    await source.act("start", "s1", "t1");
    assert.deepEqual(r.calls.map((c) => [c.url, c.method, c.body]), [
      ["/api/stop_service", "POST", { service_id: "s1", task_id: "t1" }],
      ["/api/restart_service", "POST", { service_id: "s1", task_id: "t1" }],
      ["/api/start_service", "POST", { service_id: "s1", task_id: "t1" }],
    ]);
  });

  test("a refusal keeps its code", async () => {
    const source = httpServicesSource(apiClient((async () => Response.json({ error: "no", code: "SERVICE_BUSY" }, { status: 409 })) as FetchLike), memoryFeed());
    await assert.rejects(source.act("stop", "s1", "t1"), (e: unknown) => e instanceof ServicesSourceError && e.code === "SERVICE_BUSY");
  });

  test("answers a request with the decision", async () => {
    const r = recording({ request: { id: "q1", action: "restart", state: "approved" } });
    await r.source().answer("q1", "deny");
    assert.deepEqual([r.calls[0]!.url, r.calls[0]!.body], ["/api/service_requests/q1/answer", { decision: "deny" }]);
  });

  test("follows the project's services feed; an event that does not read is dropped", () => {
    const feed = memoryFeed();
    const events: ServicesEvent[] = [];
    httpServicesSource(apiClient((async () => Response.json({})) as FetchLike), feed).follow("p1", { onEvent: (e) => events.push(e), onStatus: () => {} });
    const path = "/services/events?project_id=p1";
    assert.equal(feed.followers(path), 1);
    feed.emit(path, { seq: 3, kind: "service_changed", service: wire });
    feed.emit(path, { seq: 4, kind: "service_changed", service: { nope: 1 } });
    feed.emit(path, { seq: 5, kind: "service_deleted", service_id: "s1" });
    feed.emit(path, { seq: 6, kind: "service_request", request: { id: "q", action: "stop", state: "pending", task_id: "t1" } });
    feed.emit(path, { seq: 7, kind: "something_new" });
    assert.deepEqual(events.map((e) => [e.seq, e.kind]), [[3, "service_changed"], [5, "service_deleted"], [6, "service_request"], [7, "other"]]);
  });

  test("reads and follows a log", async () => {
    const r = recording({ text: "hi\n", offset: 0, next_offset: 3, run_id: "r1" });
    const feed = memoryFeed();
    const source = r.source(feed);
    assert.deepEqual(await source.readLog("s1"), { text: "hi\n", offset: 0, nextOffset: 3, runId: "r1" });
    assert.equal(r.calls[0]!.url, "/api/read_service_log?service_id=s1");
    const seen: unknown[] = [];
    source.followLog("s1", 3, (e) => seen.push(e), () => {});
    feed.emit("/services/log?service_id=s1&from=3", { kind: "chunk", run_id: "r1", offset: 3, text: "x" });
    assert.deepEqual(seen, [{ kind: "chunk", runId: "r1", offset: 3, text: "x" }]);
  });
});
