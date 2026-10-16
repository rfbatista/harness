import { FeedStatus } from "../domain/feed.js";
import { fakeEventSourceClass, manualTimers } from "../testing/doubles.js";
import { assert, file, test } from "../testing/test.js";
import { feed } from "./feed.js";

file("shared/infrastructure/feed");

function setup() {
  const es = fakeEventSourceClass();
  const timers = manualTimers();
  const statuses = [];
  const messages = [];
  const close = feed({ base: "/api", EventSource: es.EventSource, ...timers }).follow("/events?project_id=p1", {
    onMessage: (m) => messages.push(m),
    onStatus: (s) => statuses.push(s),
  });
  return { es, timers, statuses, messages, close };
}

test("connects, goes live, and delivers parsed messages", () => {
  const { es, statuses, messages } = setup();
  assert.equal(es.latest().url, "/api/events?project_id=p1");
  es.latest().open();
  es.latest().message({ session: { id: "s1" } });
  es.latest().message("not json");
  assert.deepEqual(statuses, [FeedStatus.CONNECTING, FeedStatus.LIVE]);
  assert.deepEqual(messages, [{ session: { id: "s1" } }]);
});

test("a drop pauses, retries with doubling backoff, and resyncs on reconnect", () => {
  const { es, timers, statuses } = setup();
  es.latest().open();
  es.latest().fail();
  assert.equal(statuses.at(-1), FeedStatus.PAUSED);
  assert.deepEqual(timers.delays(), [1000]);

  timers.tick();
  es.latest().fail();
  assert.deepEqual(timers.delays(), [2000]);

  timers.tick();
  es.latest().open();
  assert.deepEqual(statuses.slice(-2), [FeedStatus.RESYNCED, FeedStatus.LIVE]);

  es.latest().fail();
  assert.deepEqual(timers.delays(), [1000], "backoff resets after a good connection");
});

test("backoff is capped at 30s", () => {
  const { es, timers } = setup();
  for (let i = 0; i < 8; i++) {
    es.latest().fail();
    if (i < 7) timers.tick();
  }
  assert.deepEqual(timers.delays(), [30000]);
});

test("closing stops the stream and any pending retry", () => {
  const { es, timers, close } = setup();
  es.latest().fail();
  close();
  assert.deepEqual(timers.delays(), []);
  assert.ok(es.latest().closed);
});

// Followers of the same path share one stream: a page has several (the rail,
// the sessions page, the reviews band, …) and the browser allows few
// connections per host.

function shared() {
  const es = fakeEventSourceClass();
  const timers = manualTimers();
  const f = feed({ base: "/api", EventSource: es.EventSource, ...timers });
  const follower = (path = "/events?project_id=p1") => {
    const statuses = [];
    const messages = [];
    const close = f.follow(path, { onMessage: (m) => messages.push(m), onStatus: (s) => statuses.push(s) });
    return { statuses, messages, close };
  };
  return { es, timers, follower };
}

test("followers of one path share one stream and each get every message", () => {
  const { es, follower } = shared();
  const a = follower();
  const b = follower();
  assert.equal(es.instances.length, 1);
  es.latest().open();
  es.latest().message({ ticket: { id: "t1" } });
  assert.deepEqual(a.messages, [{ ticket: { id: "t1" } }]);
  assert.deepEqual(b.messages, [{ ticket: { id: "t1" } }]);
  assert.deepEqual(a.statuses, [FeedStatus.CONNECTING, FeedStatus.LIVE]);
  assert.deepEqual(b.statuses, [FeedStatus.CONNECTING, FeedStatus.LIVE]);
});

test("another path opens its own stream", () => {
  const { es, follower } = shared();
  follower("/events?project_id=p1");
  follower("/sessions/s1/events");
  assert.deepEqual(
    es.instances.map((i) => i.url),
    ["/api/events?project_id=p1", "/api/sessions/s1/events"],
  );
});

test("a follower that joins a live stream is told it is live at once", () => {
  const { es, follower } = shared();
  follower();
  es.latest().open();
  const late = follower();
  assert.deepEqual(late.statuses, [FeedStatus.LIVE]);
});

test("a follower that joins a dropped stream is told it is paused", () => {
  const { es, follower } = shared();
  follower();
  es.latest().open();
  es.latest().fail();
  const late = follower();
  assert.deepEqual(late.statuses, [FeedStatus.PAUSED]);
});

test("a drop and reconnect reach every follower", () => {
  const { es, timers, follower } = shared();
  const a = follower();
  const b = follower();
  es.latest().open();
  es.latest().fail();
  timers.tick();
  es.latest().open();
  for (const f of [a, b]) {
    assert.deepEqual(f.statuses.slice(-3), [FeedStatus.PAUSED, FeedStatus.RESYNCED, FeedStatus.LIVE]);
  }
});

test("the stream stays open until its last follower leaves", () => {
  const { es, follower } = shared();
  const a = follower();
  const b = follower();
  es.latest().open();
  a.close();
  assert.equal(es.latest().closed, false);
  es.latest().message({ ticket: { id: "t1" } });
  assert.deepEqual(a.messages, [], "a follower that left gets nothing more");
  assert.equal(b.messages.length, 1);
  b.close();
  assert.ok(es.latest().closed);
});

test("closing twice does not close the stream under another follower", () => {
  const { es, follower } = shared();
  const a = follower();
  follower();
  a.close();
  a.close();
  assert.equal(es.latest().closed, false);
});

test("the last follower leaving during a retry cancels it", () => {
  const { es, timers, follower } = shared();
  const a = follower();
  const b = follower();
  es.latest().fail();
  a.close();
  assert.deepEqual(timers.delays(), [1000]);
  b.close();
  assert.deepEqual(timers.delays(), []);
});

test("following again after everyone left opens a new stream", () => {
  const { es, follower } = shared();
  follower().close();
  follower();
  assert.equal(es.instances.length, 2);
  assert.ok(es.instances[0].closed);
  assert.equal(es.instances[1].closed, false);
});

test("a follower that throws does not keep the message from the others", () => {
  const es = fakeEventSourceClass();
  const f = feed({ base: "/api", EventSource: es.EventSource, ...manualTimers() });
  f.follow("/events", { onMessage: () => { throw new Error("boom"); } });
  const got = [];
  f.follow("/events", { onMessage: (m) => got.push(m) });
  es.latest().open();
  es.latest().message({ ticket: { id: "t1" } });
  assert.equal(got.length, 1);
});

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

test("the hello names the stream until it drops; it reaches no follower", () => {
  const es = fakeEventSourceClass();
  const timers = manualTimers();
  const f = feed({ base: "/api", EventSource: es.EventSource, ...timers });
  const got = [];
  f.followPage({ kind: "notifications" }, { onMessage: (m) => got.push(m) });
  timers.tick();
  es.latest().open();
  assert.equal(f.pageStreamId(), "");
  es.latest().message({ kind: "hello", stream_id: "st1" });
  assert.equal(f.pageStreamId(), "st1");
  es.latest().fail();
  assert.equal(f.pageStreamId(), "", "a stream that ended has no id anymore");
  assert.deepEqual(got, []);
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
