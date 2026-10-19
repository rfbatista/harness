import { FeedStatus } from "../domain/feed.js";
import { fakeEventSourceClass, fakeStreamApi, flush, manualTimers } from "../testing/doubles.js";
import { assert, file, test } from "../testing/test.js";
import { streamFeed } from "./stream-feed.js";

file("shared/infrastructure/stream-feed");

function setup() {
  const es = fakeEventSourceClass();
  const timers = manualTimers();
  const { api, posts } = fakeStreamApi();
  const f = streamFeed({ base: "/api", api, EventSource: es.EventSource, ...timers });
  const follower = (path = "/events?project_id=p1") => {
    const statuses = [];
    const messages = [];
    const close = f.follow(path, { onMessage: (m) => messages.push(m), onStatus: (s) => statuses.push(s) });
    return { statuses, messages, close };
  };
  const hello = (id = "st1") => es.latest().message({ stream: id });
  return { es, timers, api, posts, follower, hello };
}

test("every path rides one stream, followed once it is named", async () => {
  const { es, posts, follower, hello } = setup();
  const a = follower("/events?project_id=p1");
  const b = follower("/notifications/events");
  assert.equal(es.instances.length, 1);
  assert.equal(es.latest().url, "/api/stream");
  assert.deepEqual(posts, [], "nothing to follow on before the stream has an id");

  hello();
  await flush();
  assert.deepEqual(posts, [
    { route: "/stream/follow", stream: "st1", path: "/events?project_id=p1" },
    { route: "/stream/follow", stream: "st1", path: "/notifications/events" },
  ]);
  assert.deepEqual(a.statuses, [FeedStatus.CONNECTING, FeedStatus.LIVE]);
  assert.deepEqual(b.statuses, [FeedStatus.CONNECTING, FeedStatus.LIVE]);
});

test("messages reach the followers of their path only", async () => {
  const { es, follower, hello } = setup();
  const a = follower("/events?project_id=p1");
  const b = follower("/notifications/events");
  hello();
  await flush();
  es.latest().message({ path: "/notifications/events", data: { id: "n1" } });
  es.latest().message({ path: "/somewhere/else", data: { id: "x" } });
  es.latest().message("not json");
  assert.deepEqual(a.messages, []);
  assert.deepEqual(b.messages, [{ id: "n1" }]);
});

test("a path followed after the stream is named is followed at once", async () => {
  const { posts, follower, hello } = setup();
  follower("/events?project_id=p1");
  hello();
  await flush();
  const late = follower("/projects/p1/usage/events");
  await flush();
  assert.equal(posts.at(-1).stream, "st1");
  assert.deepEqual(late.statuses, [FeedStatus.CONNECTING, FeedStatus.LIVE]);
});

test("followers of one path share it; a late one is told its status", async () => {
  const { es, posts, follower, hello } = setup();
  const a = follower();
  hello();
  await flush();
  const b = follower();
  assert.equal(posts.length, 1);
  assert.deepEqual(b.statuses, [FeedStatus.LIVE]);
  es.latest().message({ path: "/events?project_id=p1", data: { ticket: { id: "t1" } } });
  assert.equal(a.messages.length, 1);
  assert.equal(b.messages.length, 1);
});

test("a feed that ends is followed again with backoff, then resynced", async () => {
  const { es, timers, posts, follower, hello } = setup();
  const a = follower();
  hello();
  await flush();
  es.latest().message({ path: "/events?project_id=p1", end: true });
  assert.equal(a.statuses.at(-1), FeedStatus.PAUSED);
  assert.deepEqual(timers.delays(), [1000]);
  timers.tick();
  await flush();
  assert.equal(posts.length, 2);
  assert.deepEqual(a.statuses.slice(-2), [FeedStatus.RESYNCED, FeedStatus.LIVE]);
});

test("a refused follow retries with doubling backoff", async () => {
  const { timers, api, follower, hello } = setup();
  const a = follower();
  api.refuse = true;
  hello();
  await flush();
  assert.equal(a.statuses.at(-1), FeedStatus.PAUSED);
  assert.deepEqual(timers.delays(), [1000]);
  timers.tick();
  await flush();
  assert.deepEqual(timers.delays(), [2000]);
  api.refuse = false;
  timers.tick();
  await flush();
  assert.deepEqual(a.statuses.slice(-2), [FeedStatus.RESYNCED, FeedStatus.LIVE]);
});

test("a dropped stream pauses every feed, reconnects with backoff, and follows them all again", async () => {
  const { es, timers, posts, follower, hello } = setup();
  const a = follower("/events?project_id=p1");
  const b = follower("/notifications/events");
  hello();
  await flush();
  es.latest().fail();
  assert.ok(es.latest().closed);
  assert.equal(a.statuses.at(-1), FeedStatus.PAUSED);
  assert.equal(b.statuses.at(-1), FeedStatus.PAUSED);
  assert.deepEqual(timers.delays(), [1000]);

  timers.tick();
  es.latest().fail();
  assert.deepEqual(timers.delays(), [2000]);

  timers.tick();
  assert.equal(es.instances.length, 3);
  hello("st2");
  await flush();
  assert.deepEqual(posts.slice(-2).map((p) => p.stream), ["st2", "st2"]);
  for (const f of [a, b]) assert.deepEqual(f.statuses.slice(-2), [FeedStatus.RESYNCED, FeedStatus.LIVE]);

  es.latest().fail();
  assert.deepEqual(timers.delays(), [1000], "backoff resets once the stream is named again");
});

test("an answer for an old stream is ignored", async () => {
  const { es, timers, follower, hello } = setup();
  const a = follower();
  hello("st1"); // its follow is still in flight when the stream drops
  es.latest().fail();
  await flush();
  assert.equal(a.statuses.at(-1), FeedStatus.PAUSED);
  timers.tick();
  hello("st2");
  await flush();
  assert.deepEqual(a.statuses.slice(-2), [FeedStatus.RESYNCED, FeedStatus.LIVE]);
});

test("the last follower of a path unfollows it; the last path closes the stream", async () => {
  const { es, posts, follower, hello } = setup();
  const a = follower("/events?project_id=p1");
  const a2 = follower("/events?project_id=p1");
  const b = follower("/notifications/events");
  hello();
  await flush();
  a.close();
  a.close();
  assert.equal(posts.length, 2, "another follower still holds the path");
  a2.close();
  assert.deepEqual(posts.at(-1), { route: "/stream/unfollow", stream: "st1", path: "/events?project_id=p1" });
  es.latest().message({ path: "/events?project_id=p1", data: { x: 1 } });
  assert.deepEqual(a2.messages, []);
  assert.equal(es.latest().closed, false);
  b.close();
  assert.ok(es.latest().closed);
});

test("leaving everything during a reconnect cancels it; following again opens a new stream", () => {
  const { es, timers, follower } = setup();
  const a = follower();
  es.latest().fail();
  assert.deepEqual(timers.delays(), [1000]);
  a.close();
  assert.deepEqual(timers.delays(), []);
  follower();
  assert.equal(es.instances.length, 2);
});

test("a follower that throws does not keep the message from the others", async () => {
  const es = fakeEventSourceClass();
  const f = streamFeed({ base: "/api", api: fakeStreamApi().api, EventSource: es.EventSource, ...manualTimers() });
  f.follow("/events", { onMessage: () => { throw new Error("boom"); } });
  const got = [];
  f.follow("/events", { onMessage: (m) => got.push(m) });
  es.latest().message({ stream: "st1" });
  await flush();
  es.latest().message({ path: "/events", data: { ticket: { id: "t1" } } });
  assert.equal(got.length, 1);
});
