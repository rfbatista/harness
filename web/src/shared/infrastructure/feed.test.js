import { StructuredError } from "../domain/errors.js";
import { FeedStatus } from "../domain/feed.js";
import { fakeEventSourceClass, flush, manualTimers } from "../testing/doubles.js";
import { assert, file, test } from "../testing/test.js";
import { feed } from "./feed.js";

file("shared/infrastructure/feed");

// The page event stream: one EventSource on /api/page_events that every
// follower of the page shares, whatever kind of change it follows. Each
// follower gets only its kind's payload, unwrapped from the envelope.

function page() {
  const es = fakeEventSourceClass();
  const timers = manualTimers();
  const f = feed({ base: "/api", EventSource: es.EventSource, ...timers });
  const follower = (topic) => {
    const statuses = [];
    const messages = [];
    const close = f.followPage(topic, { onMessage: (m) => messages.push(m), onStatus: (s) => statuses.push(s) });
    return { statuses, messages, close };
  };
  return { es, timers, follower };
}

test("project and notifications followers share one stream on /api/page_events with the project id", () => {
  const { es, timers, follower } = page();
  follower({ kind: "project", projectId: "p1" });
  follower({ kind: "notifications" });
  assert.equal(es.instances.length, 0, "the stream opens on the tick after the first follower joins");
  timers.tick();
  assert.deepEqual(es.instances.map((i) => i.url), ["/api/page_events?project_id=p1"]);
});

test("a page with no project follower opens the stream with an empty project id", () => {
  const { es, timers, follower } = page();
  follower({ kind: "notifications" });
  timers.tick();
  assert.equal(es.latest().url, "/api/page_events?project_id=");
});

test("each follower gets only its kind's payload, unwrapped from the envelope", () => {
  const { es, timers, follower } = page();
  const project = follower({ kind: "project", projectId: "p1" });
  const inbox = follower({ kind: "notifications" });
  timers.tick();
  es.latest().open();
  es.latest().message({ kind: "hello", stream_id: "st1" });
  es.latest().message({ kind: "project", change: { ticket: { id: "t1" } } });
  es.latest().message({ kind: "notifications", changed: [{ id: "n1" }], unread_count: 3 });
  assert.deepEqual(project.messages, [{ ticket: { id: "t1" } }]);
  assert.deepEqual(inbox.messages, [{ changed: [{ id: "n1" }], unread_count: 3 }]);
});

test("unknown kinds, envelopes without a kind and malformed lines are ignored", () => {
  const { es, timers, follower } = page();
  const project = follower({ kind: "project", projectId: "p1" });
  const inbox = follower({ kind: "notifications" });
  timers.tick();
  es.latest().open();
  es.latest().message({ kind: "weather", sunny: true });
  es.latest().message({ ticket: { id: "t1" } });
  es.latest().message("not json");
  es.latest().message("null");
  es.latest().message({ kind: "project", change: { ticket: { id: "t2" } } });
  assert.deepEqual(project.messages, [{ ticket: { id: "t2" } }]);
  assert.deepEqual(inbox.messages, []);
});

test("page followers hear connecting then live", () => {
  const { es, timers, follower } = page();
  const a = follower({ kind: "project", projectId: "p1" });
  const b = follower({ kind: "notifications" });
  timers.tick();
  es.latest().open();
  for (const f of [a, b]) assert.deepEqual(f.statuses, [FeedStatus.CONNECTING, FeedStatus.LIVE]);
});

test("a drop pauses every page follower, retries with backoff, and resyncs on reconnect", () => {
  const { es, timers, follower } = page();
  const a = follower({ kind: "project", projectId: "p1" });
  const b = follower({ kind: "notifications" });
  timers.tick();
  es.latest().open();
  es.latest().fail();
  assert.deepEqual(timers.delays(), [1000]);
  timers.tick();
  es.latest().fail();
  assert.deepEqual(timers.delays(), [2000]);
  timers.tick();
  es.latest().open();
  for (const f of [a, b]) {
    assert.deepEqual(f.statuses.slice(-4), [FeedStatus.PAUSED, FeedStatus.PAUSED, FeedStatus.RESYNCED, FeedStatus.LIVE]);
  }
  assert.equal(es.latest().url, "/api/page_events?project_id=p1", "it reconnects to the same stream");
  es.latest().fail();
  assert.deepEqual(timers.delays(), [1000], "backoff resets after a good connection");
});

test("the page stream's backoff is capped at 30s", () => {
  const { es, timers, follower } = page();
  follower({ kind: "notifications" });
  timers.tick();
  for (let i = 0; i < 8; i++) {
    es.latest().fail();
    if (i < 7) timers.tick();
  }
  assert.deepEqual(timers.delays(), [30000]);
});

test("a late page follower is told the stream's status at once", () => {
  const { es, timers, follower } = page();
  follower({ kind: "notifications" });
  timers.tick();
  es.latest().open();
  assert.deepEqual(follower({ kind: "project", projectId: "" }).statuses, [FeedStatus.LIVE]);
  es.latest().fail();
  assert.deepEqual(follower({ kind: "notifications" }).statuses, [FeedStatus.PAUSED]);
});

test("a late project follower for another project reconnects once with that id, as a resync", () => {
  const { es, timers, follower } = page();
  const inbox = follower({ kind: "notifications" });
  const p1 = follower({ kind: "project", projectId: "p1" });
  timers.tick();
  es.latest().open();
  const p2 = follower({ kind: "project", projectId: "p2" });
  assert.equal(es.instances.length, 2);
  assert.ok(es.instances[0].closed);
  assert.equal(es.latest().url, "/api/page_events?project_id=p2");
  es.latest().open();
  for (const f of [inbox, p1]) assert.deepEqual(f.statuses.slice(-2), [FeedStatus.RESYNCED, FeedStatus.LIVE]);
  assert.deepEqual(p2.statuses, [FeedStatus.LIVE, FeedStatus.RESYNCED, FeedStatus.LIVE]);
  es.latest().message({ kind: "project", change: { ticket: { id: "t2" } } });
  assert.deepEqual(p2.messages, [{ ticket: { id: "t2" } }]);
  assert.deepEqual(p1.messages, [], "the stream no longer carries p1's changes");
  follower({ kind: "project", projectId: "p2" });
  assert.equal(es.instances.length, 2, "another follower of the same project does not reconnect");
});

test("a project follower that joins before the stream opens sets its project without a reconnect", () => {
  const { es, timers, follower } = page();
  follower({ kind: "notifications" });
  follower({ kind: "project", projectId: "p3" });
  timers.tick();
  assert.deepEqual(es.instances.map((i) => i.url), ["/api/page_events?project_id=p3"]);
});

test("the page stream closes with its last follower, and a pending open or retry is cancelled", () => {
  const { es, timers, follower } = page();
  const a = follower({ kind: "project", projectId: "p1" });
  a.close();
  assert.deepEqual(timers.delays(), [], "the open never happens");
  assert.equal(es.instances.length, 0);

  const b = follower({ kind: "project", projectId: "p1" });
  const c = follower({ kind: "notifications" });
  timers.tick();
  es.latest().open();
  b.close();
  b.close();
  assert.equal(es.latest().closed, false);
  es.latest().message({ kind: "project", change: { ticket: { id: "t1" } } });
  assert.deepEqual(b.messages, [], "a follower that left gets nothing more");
  es.latest().fail();
  c.close();
  assert.ok(es.latest().closed);
  assert.deepEqual(timers.delays(), []);
});

test("following the page again after everyone left opens a new stream", () => {
  const { es, timers, follower } = page();
  const a = follower({ kind: "notifications" });
  timers.tick();
  a.close();
  follower({ kind: "notifications" });
  timers.tick();
  assert.equal(es.instances.length, 2);
  assert.equal(es.instances[1].closed, false);
});

test("a page follower that throws does not keep the message from the others", () => {
  const es = fakeEventSourceClass();
  const timers = manualTimers();
  const f = feed({ base: "/api", EventSource: es.EventSource, ...timers });
  f.followPage({ kind: "project", projectId: "p1" }, { onMessage: () => { throw new Error("boom"); } });
  const got = [];
  f.followPage({ kind: "project", projectId: "p1" }, { onMessage: (m) => got.push(m) });
  timers.tick();
  es.latest().open();
  es.latest().message({ kind: "project", change: { ticket: { id: "t1" } } });
  assert.equal(got.length, 1);
});

test("following an unknown kind is refused", () => {
  const { follower } = page();
  assert.throws(() => follower({ kind: "weather" }));
});

test("a project follower with no project does not move the stream off its project", () => {
  const { es, timers, follower } = page();
  const p1 = follower({ kind: "project", projectId: "p1" });
  timers.tick();
  es.latest().open();
  follower({ kind: "project", projectId: "" });
  assert.equal(es.instances.length, 1);
  es.latest().message({ kind: "project", change: { ticket: { id: "t1" } } });
  assert.equal(p1.messages.length, 1);
});

// Usage and the project catalog ride the page stream as opt-in kinds: the
// stream asks for them in `with` only when a follower wants them.

test("usage and projects followers share the page's one stream, asking for their kinds in with", () => {
  const { es, timers, follower } = page();
  const inbox = follower({ kind: "notifications" });
  const usage = follower({ kind: "usage", projectId: "p1" });
  const projects = follower({ kind: "projects" });
  timers.tick();
  assert.deepEqual(es.instances.map((i) => i.url), ["/api/page_events?project_id=p1&with=usage%2Cprojects"]);
  es.latest().open();
  es.latest().message({ kind: "usage", record: { id: "u1" } });
  es.latest().message({ kind: "projects", change: { project: { id: "p9" } } });
  assert.deepEqual(usage.messages, [{ id: "u1" }]);
  assert.deepEqual(projects.messages, [{ project: { id: "p9" } }]);
  assert.deepEqual(inbox.messages, []);
});

test("a page without usage or projects followers asks for neither", () => {
  const { es, timers, follower } = page();
  follower({ kind: "project", projectId: "p1" });
  timers.tick();
  assert.equal(es.latest().url, "/api/page_events?project_id=p1");
});

test("a usage follower shares the stream of a project follower of its project", () => {
  const { es, timers, follower } = page();
  const project = follower({ kind: "project", projectId: "p1" });
  const usage = follower({ kind: "usage", projectId: "p1" });
  timers.tick();
  assert.deepEqual(es.instances.map((i) => i.url), ["/api/page_events?project_id=p1&with=usage"]);
  es.latest().open();
  es.latest().message({ kind: "project", change: { ticket: { id: "t1" } } });
  es.latest().message({ kind: "usage", record: { id: "u1" } });
  assert.deepEqual(project.messages, [{ ticket: { id: "t1" } }]);
  assert.deepEqual(usage.messages, [{ id: "u1" }]);
});

test("a usage follower with no project does not move the stream off its project", () => {
  const { es, timers, follower } = page();
  follower({ kind: "project", projectId: "p1" });
  follower({ kind: "usage", projectId: "" });
  timers.tick();
  assert.equal(es.latest().url, "/api/page_events?project_id=p1&with=usage");
});

test("a late follower needing a new kind reconnects once, as a resync", () => {
  const { es, timers, follower } = page();
  const inbox = follower({ kind: "notifications" });
  timers.tick();
  es.latest().open();
  const projects = follower({ kind: "projects" });
  assert.equal(es.instances.length, 2);
  assert.ok(es.instances[0].closed);
  assert.equal(es.latest().url, "/api/page_events?project_id=&with=projects");
  es.latest().open();
  assert.deepEqual(inbox.statuses.slice(-2), [FeedStatus.RESYNCED, FeedStatus.LIVE]);
  assert.deepEqual(projects.statuses, [FeedStatus.LIVE, FeedStatus.RESYNCED, FeedStatus.LIVE]);
  follower({ kind: "projects" });
  assert.equal(es.instances.length, 2, "another follower of a kind the stream carries does not reconnect");
  const usage = follower({ kind: "usage", projectId: "p1" });
  assert.equal(es.instances.length, 3);
  assert.equal(es.latest().url, "/api/page_events?project_id=p1&with=usage%2Cprojects");
  es.latest().open();
  es.latest().message({ kind: "usage", record: { id: "u1" } });
  assert.deepEqual(usage.messages, [{ id: "u1" }]);
});

test("a kind stays on the stream after its followers leave, so a later one does not reconnect", () => {
  const { es, timers, follower } = page();
  follower({ kind: "notifications" });
  const projects = follower({ kind: "projects" });
  timers.tick();
  es.latest().open();
  projects.close();
  follower({ kind: "projects" });
  assert.equal(es.instances.length, 1);
});

// The session kind: the page follows at most one session on its stream, the
// one its latest session follower asked for, with POST /api/follow_session
// once the hello has named the stream.

function sessions() {
  const es = fakeEventSourceClass();
  const timers = manualTimers();
  const posts = [];
  /** Answers the oldest post still waiting: ok, with the code it fails with, or with the error it throws. */
  const answer = (failure) => {
    const p = posts.find((x) => !x.answered);
    p.answered = true;
    if (failure instanceof Error) p.reject(failure);
    else if (failure) p.reject(new StructuredError(failure, failure, 404));
    else p.resolve(null);
    return flush();
  };
  const post = (path, body) =>
    new Promise((resolve, reject) => posts.push({ path, body, resolve, reject, answered: false }));
  const f = feed({ base: "/api", EventSource: es.EventSource, post, ...timers });
  const follower = (topic) => {
    const messages = [];
    const statuses = [];
    const close = f.followPage(topic, { onMessage: (m) => messages.push(m), onStatus: (s) => statuses.push(s) });
    return { messages, statuses, close };
  };
  const sent = () => posts.map((p) => [p.path, p.body]);
  return { es, timers, follower, posts, sent, answer, f };
}

test("a session follower is followed once the hello names the stream, never before", async () => {
  const { es, timers, follower, sent, answer } = sessions();
  follower({ kind: "session", sessionId: "s1" });
  timers.tick();
  es.latest().open();
  assert.deepEqual(sent(), [], "no follow before the hello");
  es.latest().message({ kind: "hello", stream_id: "st1" });
  assert.deepEqual(sent(), [["/follow_session", { stream_id: "st1", session_id: "s1" }]]);
  await answer();
  assert.equal(sent().length, 1);
  assert.deepEqual(es.instances.map((i) => i.url), ["/api/page_events?project_id="], "the session rides the one page stream");
});

test("the hello reaches no follower; after a drop a session waits for the next stream's hello", () => {
  const { es, timers, follower, sent } = sessions();
  const inbox = follower({ kind: "notifications" });
  timers.tick();
  es.latest().open();
  es.latest().message({ kind: "hello", stream_id: "st1" });
  assert.deepEqual(inbox.messages, []);
  es.latest().fail();
  follower({ kind: "session", sessionId: "s1" });
  assert.deepEqual(sent(), [], "the stream that ended is not named anymore");
  timers.tick();
  es.latest().open();
  es.latest().message({ kind: "hello", stream_id: "st2" });
  assert.deepEqual(sent(), [["/follow_session", { stream_id: "st2", session_id: "s1" }]]);
});

test("a session follower gets its session's events, unwrapped, and nothing of another session", () => {
  const { es, timers, follower } = sessions();
  const s1 = follower({ kind: "session", sessionId: "s1" });
  const inbox = follower({ kind: "notifications" });
  timers.tick();
  es.latest().open();
  es.latest().message({ kind: "hello", stream_id: "st1" });
  es.latest().message({ kind: "session", session_id: "s1", event: { seq: 1, type: "artifact" } });
  es.latest().message({ kind: "session", session_id: "s9", event: { seq: 7, type: "artifact" } });
  assert.deepEqual(s1.messages, [{ seq: 1, type: "artifact" }]);
  assert.deepEqual(inbox.messages, []);
});

test("the latest session follower wins; switching posts the new session on the same stream", async () => {
  const { es, timers, follower, sent, answer } = sessions();
  const s1 = follower({ kind: "session", sessionId: "s1" });
  timers.tick();
  es.latest().open();
  es.latest().message({ kind: "hello", stream_id: "st1" });
  await answer();
  const s2 = follower({ kind: "session", sessionId: "s2" });
  assert.deepEqual(sent().at(-1), ["/follow_session", { stream_id: "st1", session_id: "s2" }]);
  await answer();
  s1.close();
  assert.equal(sent().length, 2, "an older follower leaving changes nothing");
  es.latest().message({ kind: "session", session_id: "s2", event: { seq: 1 } });
  assert.deepEqual(s2.messages, [{ seq: 1 }]);
  assert.equal(es.instances.length, 1, "no new connection");
});

test("the last session follower leaving stops following, while the stream stays open for the others", async () => {
  const { es, timers, follower, sent, answer } = sessions();
  follower({ kind: "notifications" });
  const s1 = follower({ kind: "session", sessionId: "s1" });
  timers.tick();
  es.latest().open();
  es.latest().message({ kind: "hello", stream_id: "st1" });
  await answer();
  s1.close();
  assert.deepEqual(sent().at(-1), ["/follow_session", { stream_id: "st1", session_id: null }]);
  assert.equal(es.latest().closed, false);
});

test("a session follower leaving a stream that is not open posts nothing", async () => {
  const { es, timers, follower, sent } = sessions();
  follower({ kind: "notifications" });
  const s1 = follower({ kind: "session", sessionId: "s1" });
  timers.tick();
  es.latest().open();
  s1.close();
  es.latest().message({ kind: "hello", stream_id: "st1" });
  assert.deepEqual(sent(), []);
});

test("a switch while a follow is in flight posts after its answer, the latest session only", async () => {
  const { es, timers, follower, sent, answer } = sessions();
  follower({ kind: "session", sessionId: "s1" });
  timers.tick();
  es.latest().open();
  es.latest().message({ kind: "hello", stream_id: "st1" });
  follower({ kind: "session", sessionId: "s2" });
  follower({ kind: "session", sessionId: "s3" });
  assert.equal(sent().length, 1, "one follow at a time, so they arrive in order");
  await answer();
  assert.deepEqual(sent().map(([, b]) => b.session_id), ["s1", "s3"]);
});

test("after a reconnect the session is followed again, after the highest seq delivered", async () => {
  const { es, timers, follower, sent, answer } = sessions();
  follower({ kind: "session", sessionId: "s1" });
  timers.tick();
  es.latest().open();
  es.latest().message({ kind: "hello", stream_id: "st1" });
  await answer();
  es.latest().message({ kind: "session", session_id: "s1", event: { seq: 4 } });
  es.latest().message({ kind: "session", session_id: "s1", event: { seq: 9 } });
  es.latest().message({ kind: "session", session_id: "s1", event: { seq: 7 } });
  es.latest().fail();
  timers.tick();
  es.latest().open();
  assert.equal(sent().length, 1, "no follow before the new stream's hello");
  es.latest().message({ kind: "hello", stream_id: "st2" });
  assert.deepEqual(sent().at(-1), ["/follow_session", { stream_id: "st2", session_id: "s1", after_seq: 9 }]);
});

test("a follow answered STREAM_NOT_FOUND is not retried until the next hello", async () => {
  const { es, timers, follower, sent, answer } = sessions();
  follower({ kind: "session", sessionId: "s1" });
  timers.tick();
  es.latest().open();
  es.latest().message({ kind: "hello", stream_id: "st1" });
  await answer("STREAM_NOT_FOUND");
  await flush();
  assert.equal(sent().length, 1, "no retry in a loop");
  assert.deepEqual(timers.delays(), []);
  es.latest().fail();
  timers.tick();
  es.latest().open();
  es.latest().message({ kind: "hello", stream_id: "st2" });
  assert.deepEqual(sent().at(-1), ["/follow_session", { stream_id: "st2", session_id: "s1" }]);
});

test("a follow answered SESSION_NOT_FOUND is not retried: the session is gone", async () => {
  const { es, timers, follower, sent, answer } = sessions();
  const gone = follower({ kind: "session", sessionId: "gone" });
  timers.tick();
  es.latest().open();
  es.latest().message({ kind: "hello", stream_id: "st1" });
  await answer("SESSION_NOT_FOUND");
  follower({ kind: "notifications" });
  await flush();
  assert.equal(sent().length, 1);
  assert.deepEqual(timers.delays(), []);
  assert.deepEqual(gone.statuses, [FeedStatus.CONNECTING, FeedStatus.LIVE]);
});

// A follow that fails on the way (the network, a 5xx) would leave the session
// followers deaf on a live stream: they are paused, and the follow is retried
// on the same stream with the stream's backoff until it lands.

test("a follow that fails on the way pauses the session's followers and is retried with backoff; landing resyncs them", async () => {
  const { es, timers, follower, sent, answer } = sessions();
  const s1 = follower({ kind: "session", sessionId: "s1" });
  const inbox = follower({ kind: "notifications" });
  timers.tick();
  es.latest().open();
  es.latest().message({ kind: "hello", stream_id: "st1" });
  await answer(new TypeError("Failed to fetch"));
  assert.equal(s1.statuses.at(-1), FeedStatus.PAUSED);
  assert.equal(inbox.statuses.at(-1), FeedStatus.LIVE, "only the session's followers missed something");
  assert.deepEqual(timers.delays(), [1000]);
  timers.tick();
  assert.deepEqual(sent().at(-1), ["/follow_session", { stream_id: "st1", session_id: "s1" }]);
  await answer(new StructuredError("UNAVAILABLE", "unavailable", 503));
  assert.deepEqual(timers.delays(), [2000]);
  timers.tick();
  assert.equal(sent().length, 3);
  await answer();
  assert.deepEqual(s1.statuses.slice(-2), [FeedStatus.RESYNCED, FeedStatus.LIVE]);
  assert.deepEqual(timers.delays(), []);
  assert.equal(es.instances.length, 1, "the same stream all along");
  assert.deepEqual(inbox.statuses, [FeedStatus.CONNECTING, FeedStatus.LIVE]);
});

test("a retried follow asks for what came after the highest seq delivered", async () => {
  const { es, timers, follower, sent, answer } = sessions();
  follower({ kind: "session", sessionId: "s1" });
  timers.tick();
  es.latest().open();
  es.latest().message({ kind: "hello", stream_id: "st1" });
  await answer();
  es.latest().message({ kind: "session", session_id: "s1", event: { seq: 5 } });
  es.latest().fail();
  timers.tick();
  es.latest().open();
  es.latest().message({ kind: "hello", stream_id: "st2" });
  await answer(new TypeError("Failed to fetch"));
  timers.tick();
  assert.deepEqual(sent().at(-1), ["/follow_session", { stream_id: "st2", session_id: "s1", after_seq: 5 }]);
});

test("the follow retry's backoff is capped at 30s and resets once a follow lands", async () => {
  const { es, timers, follower, answer } = sessions();
  follower({ kind: "session", sessionId: "s1" });
  timers.tick();
  es.latest().open();
  es.latest().message({ kind: "hello", stream_id: "st1" });
  for (let i = 0; i < 7; i++) {
    await answer(new TypeError("Failed to fetch"));
    timers.tick();
  }
  await answer(new TypeError("Failed to fetch"));
  assert.deepEqual(timers.delays(), [30000]);
  timers.tick();
  await answer();
  es.latest().fail();
  timers.tick();
  es.latest().open();
  es.latest().message({ kind: "hello", stream_id: "st2" });
  await answer(new TypeError("Failed to fetch"));
  assert.deepEqual(timers.delays(), [1000]);
});

test("a late follower of a session whose follow is failing is told it is paused", async () => {
  const { es, timers, follower, answer } = sessions();
  follower({ kind: "session", sessionId: "s1" });
  timers.tick();
  es.latest().open();
  es.latest().message({ kind: "hello", stream_id: "st1" });
  await answer(new TypeError("Failed to fetch"));
  assert.deepEqual(follower({ kind: "session", sessionId: "s1" }).statuses, [FeedStatus.LIVE, FeedStatus.PAUSED]);
});

test("a follow retry is dropped when the session changes", async () => {
  const { es, timers, follower, sent, answer } = sessions();
  follower({ kind: "session", sessionId: "s1" });
  timers.tick();
  es.latest().open();
  es.latest().message({ kind: "hello", stream_id: "st1" });
  await answer(new TypeError("Failed to fetch"));
  const s2 = follower({ kind: "session", sessionId: "s2" });
  assert.deepEqual(timers.delays(), []);
  assert.deepEqual(sent().at(-1), ["/follow_session", { stream_id: "st1", session_id: "s2" }]);
  await answer();
  assert.deepEqual(s2.statuses, [FeedStatus.LIVE], "the new session's follow never failed");
});

test("a follow retry is dropped when the last session follower leaves", async () => {
  const { es, timers, follower, sent, answer } = sessions();
  follower({ kind: "notifications" });
  const s1 = follower({ kind: "session", sessionId: "s1" });
  timers.tick();
  es.latest().open();
  es.latest().message({ kind: "hello", stream_id: "st1" });
  await answer(new TypeError("Failed to fetch"));
  s1.close();
  assert.deepEqual(timers.delays(), []);
  assert.deepEqual(sent().at(-1), ["/follow_session", { stream_id: "st1", session_id: null }]);
});

test("a follow retry is dropped when the stream drops: the next hello follows again", async () => {
  const { es, timers, follower, sent, answer } = sessions();
  follower({ kind: "session", sessionId: "s1" });
  timers.tick();
  es.latest().open();
  es.latest().message({ kind: "hello", stream_id: "st1" });
  await answer(new TypeError("Failed to fetch"));
  es.latest().fail();
  assert.deepEqual(timers.delays(), [1000], "only the stream's own retry is left");
  timers.tick();
  assert.equal(sent().length, 1);
  es.latest().open();
  es.latest().message({ kind: "hello", stream_id: "st2" });
  assert.deepEqual(sent().at(-1), ["/follow_session", { stream_id: "st2", session_id: "s1" }]);
});

test("a follow retry is dropped when every follower leaves", async () => {
  const { es, timers, follower, answer } = sessions();
  const s1 = follower({ kind: "session", sessionId: "s1" });
  timers.tick();
  es.latest().open();
  es.latest().message({ kind: "hello", stream_id: "st1" });
  await answer(new TypeError("Failed to fetch"));
  s1.close();
  assert.deepEqual(timers.delays(), []);
});
