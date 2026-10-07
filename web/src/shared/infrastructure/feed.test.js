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
