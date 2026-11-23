import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { apiClient, type FetchLike } from "../../../shared/infrastructure/api-client";
import { memoryFeed } from "../../../shared/infrastructure/memory-feed";
import { BoardSourceError } from "../application/ports";
import { httpBoardSource } from "./http-board-source";

const answering = (respond: () => Response) => httpBoardSource(apiClient((async () => respond()) as FetchLike), memoryFeed());
const failsWith = (code: string) => (err: unknown) => err instanceof BoardSourceError && err.code === code;

const wire = { id: "s1", status: "running", role: "delegate", parent_session_id: "s0", brought_under: true, agent_id: "a1", created_at: "2026-11-23T12:00:00Z", updated_at: "2026-11-23T12:01:00Z", unknown_extra: 1 };

describe("HTTP board source: sessions", () => {
  test("reads the sessions of the task and asks for that task only", async () => {
    const urls: string[] = [];
    const source = httpBoardSource(apiClient((async (url: string) => (urls.push(url), Response.json({ sessions: [wire] }))) as FetchLike), memoryFeed());
    const [s] = await source.sessions("t1");
    assert.deepEqual(urls, ["/api/sessions?ticket_id=t1"]);
    assert.equal(s?.id, "s1");
    assert.equal(s?.parentSessionId, "s0");
    assert.equal(s?.broughtUnder, true);
    assert.equal(s?.createdAt.toISOString(), "2026-11-23T12:00:00.000Z");
  });

  test("an answer that is not a session list is BAD_RESPONSE", async () => {
    await assert.rejects(answering(() => Response.json({ nope: 1 })).sessions("t1"), failsWith("BAD_RESPONSE"));
  });

  test("a session with an unknown status or no created_at is BAD_RESPONSE", async () => {
    await assert.rejects(answering(() => Response.json({ sessions: [{ ...wire, status: "bogus" }] })).sessions("t1"), failsWith("BAD_RESPONSE"));
    await assert.rejects(answering(() => Response.json({ sessions: [{ ...wire, created_at: undefined }] })).sessions("t1"), failsWith("BAD_RESPONSE"));
  });

  test("a failing server is UNKNOWN, a missing network is NETWORK_ERROR", async () => {
    await assert.rejects(answering(() => new Response("oops", { status: 500 })).sessions("t1"), failsWith("UNKNOWN"));
    const down = httpBoardSource(apiClient((async () => { throw new TypeError("offline"); }) as FetchLike), memoryFeed());
    await assert.rejects(down.sessions("t1"), failsWith("NETWORK_ERROR"));
  });
});

describe("HTTP board source: messages and review requests", () => {
  const message = { id: "m1", kind: "question", from_session_id: "s1", to_session_id: "s0", created_at: "2026-11-23T12:00:00Z", status: "", delivered: true };
  const review = { id: "r1", state: "pending", architect_session_id: "s0", created_at: "2026-11-23T12:00:00Z" };

  test("reads the task's messages and its pending review requests", async () => {
    const urls: string[] = [];
    const source = httpBoardSource(apiClient((async (url: string) => (urls.push(url), Response.json(url.includes("task_messages") ? { messages: [message] } : { review_requests: [review] }))) as FetchLike), memoryFeed());
    const [m] = await source.messages("t1");
    const [r] = await source.pendingReviews("t1");
    assert.deepEqual(urls, ["/api/task_messages?ticket_id=t1", "/api/review_requests?ticket_id=t1&state=pending"]);
    assert.equal(m?.kind, "question");
    assert.equal(m?.fromSessionId, "s1");
    assert.equal(r?.architectSessionId, "s0");
  });

  test("a message of an unknown kind, or a review of an unknown state, is BAD_RESPONSE", async () => {
    await assert.rejects(answering(() => Response.json({ messages: [{ ...message, kind: "shout" }] })).messages("t1"), failsWith("BAD_RESPONSE"));
    await assert.rejects(answering(() => Response.json({ review_requests: [{ ...review, state: "maybe" }] })).pendingReviews("t1"), failsWith("BAD_RESPONSE"));
    await assert.rejects(answering(() => Response.json({})).messages("t1"), failsWith("BAD_RESPONSE"));
  });
});

describe("HTTP board source: the project feed", () => {
  const wireSession = { id: "s2", ticket_id: "t1", status: "idle", created_at: "2026-11-23T12:00:00Z", updated_at: "2026-11-23T12:05:00Z" };

  function follow() {
    const feed = memoryFeed();
    const events: unknown[] = [];
    const statuses: string[] = [];
    const stop = httpBoardSource(apiClient((async () => Response.json({})) as FetchLike), feed).follow("p/1", { onEvent: (e) => events.push(e), onStatus: (s) => statuses.push(s) });
    return { feed, events, statuses, stop };
  }

  test("follows the project's event stream, and stops", () => {
    const { feed, stop } = follow();
    assert.equal(feed.followers("/events?project_id=p%2F1"), 1);
    stop();
    assert.equal(feed.followers("/events?project_id=p%2F1"), 0);
  });

  test("reads session, task message and review request messages, and deleted sessions", () => {
    const { feed, events } = follow();
    const path = "/events?project_id=p%2F1";
    feed.emit(path, { session: wireSession });
    feed.emit(path, { task_message: { id: "m1", task_id: "t1", kind: "question", created_at: "2026-11-23T12:00:00Z" } });
    feed.emit(path, { review_request: { id: "r1", task_id: "t1", state: "pending", created_at: "2026-11-23T12:00:00Z" } });
    feed.emit(path, { deleted: true, session: { id: "s2" } });
    assert.deepEqual((events as { kind: string }[]).map((e) => e.kind), ["session", "message", "review", "session-deleted"]);
  });

  test("ignores kinds it does not draw and drops messages it cannot read", () => {
    const { feed, events } = follow();
    const path = "/events?project_id=p%2F1";
    feed.emit(path, { artifact: { id: "a1" } });
    feed.emit(path, { session: { id: "s2", status: "bogus" } });
    feed.emit(path, "garbage");
    assert.deepEqual(events, []);
  });

  test("passes the stream's state on", () => {
    const { feed, statuses } = follow();
    feed.setStatus("/events?project_id=p%2F1", "paused");
    assert.deepEqual(statuses, ["connecting", "paused"]);
  });
});
