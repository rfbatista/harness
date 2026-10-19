import { FeedStatus } from "../domain/feed.js";
import { fakeCrossTab, fakeEventSourceClass, fakeStreamApi, flush, manualTimers } from "../testing/doubles.js";
import { assert, file, test } from "../testing/test.js";
import { sharedFeed } from "./shared-feed.js";

file("shared/infrastructure/shared-feed");

function followerOn(f, path) {
  const statuses = [];
  const messages = [];
  const close = f.follow(path, { onMessage: (m) => messages.push(m), onStatus: (s) => statuses.push(s) });
  return { statuses, messages, close };
}

test("without the cross-tab APIs a tab keeps its own stream, as before", async () => {
  for (const missing of [{ locks: null }, { BroadcastChannel: null }]) {
    const es = fakeEventSourceClass();
    const timers = manualTimers();
    const { api, posts } = fakeStreamApi();
    const f = sharedFeed({ base: "/api", api, EventSource: es.EventSource, ...timers, locks: {}, BroadcastChannel: class {}, ...missing });
    const a = followerOn(f, "/events?project_id=p1");
    assert.equal(es.instances.length, 1);
    assert.equal(es.latest().url, "/api/stream");
    es.latest().message({ stream: "st1" });
    await flush();
    assert.deepEqual(posts, [{ route: "/stream/follow", stream: "st1", path: "/events?project_id=p1" }]);
    assert.deepEqual(a.statuses, [FeedStatus.CONNECTING, FeedStatus.LIVE]);

    es.latest().message({ path: "/events?project_id=p1", data: { x: 1 } });
    assert.deepEqual(a.messages, [{ x: 1 }]);

    es.latest().message({ path: "/events?project_id=p1", end: true });
    assert.equal(a.statuses.at(-1), FeedStatus.PAUSED);
    assert.deepEqual(timers.delays(), [1000]);
    timers.tick();
    await flush();
    assert.deepEqual(a.statuses.slice(-2), [FeedStatus.RESYNCED, FeedStatus.LIVE]);

    a.close();
    assert.ok(es.latest().closed);
  }
});

// Tabs of one browser profile, each with its own shared feed. They share
// the server's api and count EventSources together.
function browserProfile() {
  const cross = fakeCrossTab();
  const es = fakeEventSourceClass();
  const timers = manualTimers();
  const { api, posts } = fakeStreamApi();
  // One clock and one heartbeat for all tabs: beat() moves the clock on and
  // runs every tab's interval once, frozen tabs' too (they check holding).
  let clock = 0;
  const intervals = new Map();
  let nextInterval = 1;
  const heartbeat = {
    now: () => clock,
    setInterval(fn) {
      const id = nextInterval++;
      intervals.set(id, fn);
      return id;
    },
    clearInterval: (id) => intervals.delete(id),
  };
  const later = (ms) => {
    clock += ms;
  };
  const beat = (ms) => {
    clock += ms;
    for (const fn of [...intervals.values()]) fn();
  };
  const tabs = [];
  function open() {
    const t = cross.tab();
    const mine = fakeEventSourceClass();
    // Each tab's EventSources are counted in `es` too, and close with the tab.
    class TabEventSource extends mine.EventSource {
      constructor(url) {
        super(url);
        es.instances.push(this);
      }
    }
    let frozen = false;
    const f = sharedFeed({
      base: "/api",
      api,
      EventSource: TabEventSource,
      ...timers,
      locks: t.locks,
      BroadcastChannel: t.BroadcastChannel,
      window: t.window,
      now: heartbeat.now,
      // A frozen tab's interval does not run.
      setInterval: (fn, ms) => heartbeat.setInterval(() => frozen || fn(), ms),
      clearInterval: heartbeat.clearInterval,
    });
    const tab = {
      feed: f,
      follow: (path) => followerOn(f, path),
      close(opts) {
        t.close(opts);
        for (const s of mine.instances) s.closed = true;
      },
      streams: mine.instances,
      fire: (type, event) => t.fire(type, event),
      freeze() {
        frozen = true;
        t.freeze();
      },
      thaw() {
        frozen = false;
        t.thaw();
      },
    };
    tabs.push(tab);
    return tab;
  }
  const openStreams = () => es.instances.filter((s) => !s.closed);
  const stream = () => {
    const open = openStreams();
    assert.equal(open.length, 1, "exactly one open stream");
    return open[0];
  };
  const hello = (id = "st1") => stream().message({ stream: id });
  return { open, tabs, es, timers, api, posts, openStreams, stream, hello, beat, later };
}

const P1 = "/events?project_id=p1";
const INBOX = "/notifications/events";

test("however many tabs follow, one stream is open, in the stream-holding tab", async () => {
  const b = browserProfile();
  const tabs = [b.open(), b.open(), b.open()];
  tabs.forEach((t) => t.follow(P1));
  await flush();
  assert.equal(b.es.instances.length, 1);
  assert.equal(tabs[0].streams.length, 1, "the first tab holds the stream");
  assert.equal(b.stream().url, "/api/stream");
});

test("a path two tabs follow is followed once, and unfollowed only when the last tab leaves it", async () => {
  const b = browserProfile();
  const [one, two] = [b.open(), b.open()];
  const a = one.follow(P1);
  const c = two.follow(P1);
  two.follow(INBOX);
  await flush();
  b.hello();
  await flush();
  assert.deepEqual(b.posts, [
    { route: "/stream/follow", stream: "st1", path: P1 },
    { route: "/stream/follow", stream: "st1", path: INBOX },
  ]);
  assert.deepEqual(a.statuses, [FeedStatus.CONNECTING, FeedStatus.LIVE]);
  assert.deepEqual(c.statuses, [FeedStatus.CONNECTING, FeedStatus.LIVE]);

  c.close();
  await flush();
  assert.equal(b.posts.length, 2, "the other tab still follows it");
  a.close();
  await flush();
  assert.deepEqual(b.posts.at(-1), { route: "/stream/unfollow", stream: "st1", path: P1 });
  assert.equal(b.posts.length, 3);
});

test("an event reaches the followers of its path in every tab, and no one else", async () => {
  const b = browserProfile();
  const [one, two, three] = [b.open(), b.open(), b.open()];
  const a = one.follow(P1);
  const c = two.follow(P1);
  const d = three.follow(INBOX);
  await flush();
  b.hello();
  await flush();
  b.stream().message({ path: P1, data: { ticket: { id: "t1" } } });
  b.stream().message({ path: "/somewhere/else", data: { x: 1 } });
  assert.deepEqual(a.messages, [{ ticket: { id: "t1" } }]);
  assert.deepEqual(c.messages, [{ ticket: { id: "t1" } }]);
  assert.deepEqual(d.messages, []);
});

test("a tab joining a live path is told live at once, without a new follow", async () => {
  const b = browserProfile();
  const [one, two] = [b.open(), b.open()];
  one.follow(P1);
  b.hello();
  await flush();
  const late = two.follow(P1);
  await flush();
  assert.equal(b.posts.length, 1);
  assert.deepEqual(late.statuses, [FeedStatus.CONNECTING, FeedStatus.LIVE]);
});

test("a tab that closes releases its paths", async () => {
  const b = browserProfile();
  const [one, two] = [b.open(), b.open()];
  one.follow(INBOX);
  two.follow(P1);
  await flush();
  b.hello();
  await flush();
  two.close();
  await flush();
  assert.deepEqual(b.posts.at(-1), { route: "/stream/unfollow", stream: "st1", path: P1 });
});

for (const crash of [false, true]) {
  test(`when the stream-holding tab ${crash ? "crashes" : "closes"}, another tab takes the stream over and every tab resyncs`, async () => {
    const b = browserProfile();
    const [one, two, three] = [b.open(), b.open(), b.open()];
    one.follow(INBOX);
    const a = two.follow(P1);
    const c = three.follow(P1);
    const d = three.follow(INBOX);
    await flush();
    b.hello();
    await flush();
    b.later(60_000); // a quiet spell: the handover is at once

    one.close({ crash });
    await flush();
    assert.equal(two.streams.length, 1, "the next tab in line opens a stream");
    for (const f of [a, c, d]) assert.equal(f.statuses.at(-1), FeedStatus.PAUSED);

    b.hello("st2");
    await flush();
    const followed = b.posts.filter((p) => p.stream === "st2").map((p) => `${p.route} ${p.path}`);
    assert.deepEqual(followed.sort(), [`/stream/follow ${INBOX}`, `/stream/follow ${P1}`].sort());
    for (const f of [a, c, d]) assert.deepEqual(f.statuses.slice(-3), [FeedStatus.PAUSED, FeedStatus.RESYNCED, FeedStatus.LIVE]);

    b.stream().message({ path: P1, data: { n: 1 } });
    assert.deepEqual(a.messages, [{ n: 1 }]);
    assert.deepEqual(c.messages, [{ n: 1 }]);
  });
}

test("a feed that ends pauses its followers in every tab, follows again with backoff, then resyncs; other feeds stay live", async () => {
  const b = browserProfile();
  const [one, two] = [b.open(), b.open()];
  const a = one.follow(P1);
  const c = two.follow(P1);
  const d = two.follow(INBOX);
  await flush();
  b.hello();
  await flush();

  b.stream().message({ path: P1, end: true });
  for (const f of [a, c]) assert.equal(f.statuses.at(-1), FeedStatus.PAUSED);
  assert.equal(d.statuses.at(-1), FeedStatus.LIVE);
  assert.deepEqual(b.timers.delays(), [1000]);

  b.api.refuse = true;
  b.timers.tick();
  await flush();
  assert.deepEqual(b.timers.delays(), [2000], "the backoff doubles");
  b.api.refuse = false;
  b.timers.tick();
  await flush();
  for (const f of [a, c]) assert.deepEqual(f.statuses.slice(-3), [FeedStatus.PAUSED, FeedStatus.RESYNCED, FeedStatus.LIVE]);
  assert.deepEqual(d.statuses, [FeedStatus.CONNECTING, FeedStatus.LIVE]);
});

test("a dropped stream pauses every feed in every tab, reconnects with backoff, and resyncs them all", async () => {
  const b = browserProfile();
  const [one, two] = [b.open(), b.open()];
  const a = one.follow(P1);
  const c = two.follow(INBOX);
  await flush();
  b.hello();
  await flush();

  b.stream().fail();
  for (const f of [a, c]) assert.equal(f.statuses.at(-1), FeedStatus.PAUSED);
  assert.deepEqual(b.timers.delays(), [1000]);
  b.timers.tick();
  b.hello("st2");
  await flush();
  assert.deepEqual(b.posts.filter((p) => p.stream === "st2").length, 2);
  for (const f of [a, c]) assert.deepEqual(f.statuses.slice(-3), [FeedStatus.PAUSED, FeedStatus.RESYNCED, FeedStatus.LIVE]);
});

test("a coalesced session output delta reaches every tab unchanged", async () => {
  const b = browserProfile();
  const [one, two] = [b.open(), b.open()];
  const path = "/sessions/s1/events";
  const a = one.follow(path);
  const c = two.follow(path);
  await flush();
  b.hello();
  await flush();
  const delta = { seq: 0, session_id: "s1", type: "output_delta", delta_kind: "text", text: "first write, second write, third" };
  b.stream().message({ path, data: delta });
  assert.deepEqual(a.messages, [delta]);
  assert.deepEqual(c.messages, [delta]);
});

// Holds the stream in one, followed from every tab, live on st1.
async function threeLiveTabs() {
  const b = browserProfile();
  const [one, two, three] = [b.open(), b.open(), b.open()];
  const z = one.follow(INBOX);
  const a = two.follow(P1);
  const c = three.follow(P1);
  await flush();
  b.hello();
  await flush();
  b.later(60_000);
  return { b, one, two, three, z, a, c };
}

test("a frozen stream-holding tab is replaced by exactly one tab, and every tab resyncs", async () => {
  const { b, one, two, three, a, c } = await threeLiveTabs();
  one.freeze();
  for (let s = 0; s < 8; s += 2) b.beat(2000);
  assert.equal(two.streams.length, 0, "eight seconds of silence is not enough");
  for (let s = 0; s < 4; s += 2) b.beat(2000);
  await flush();

  const taken = [two, three].filter((t) => t.streams.length > 0);
  assert.equal(taken.length, 1, "exactly one tab takes the stream over");
  const fresh = taken[0].streams[0];
  assert.equal(fresh.closed, false);
  for (const f of [a, c]) assert.equal(f.statuses.at(-1), FeedStatus.PAUSED);

  fresh.message({ stream: "st2" });
  await flush();
  for (const f of [a, c]) assert.deepEqual(f.statuses.slice(-3), [FeedStatus.PAUSED, FeedStatus.RESYNCED, FeedStatus.LIVE]);
  fresh.message({ path: P1, data: { n: 1 } });
  assert.deepEqual(a.messages, [{ n: 1 }]);
  assert.deepEqual(c.messages, [{ n: 1 }]);
  for (let s = 0; s < 20; s += 2) b.beat(2000);
  assert.equal(b.es.instances.length, 2, "the new stream-holding tab answers, so no one takes over again");
});

test("a stream-holding tab that answers late but within the window is not replaced", async () => {
  const { b, one, two, three } = await threeLiveTabs();
  one.freeze();
  for (let s = 0; s < 6; s += 2) b.beat(2000);
  one.thaw();
  for (let s = 0; s < 30; s += 2) b.beat(2000);
  await flush();
  assert.equal(b.es.instances.length, 1);
  assert.equal(two.streams.length + three.streams.length, 0);
});

test("a deposed stream-holding tab that wakes up closes its stream, stops relaying, and resyncs through the new one", async () => {
  const { b, one, two, three, z, a } = await threeLiveTabs();
  const old = one.streams[0];
  one.freeze();
  for (let s = 0; s < 12; s += 2) b.beat(2000);
  await flush();
  const taken = [two, three].find((t) => t.streams.length > 0);
  taken.streams[0].message({ stream: "st2" });
  await flush();
  assert.equal(b.openStreams().length, 2, "the frozen tab cannot close its stream yet");

  one.thaw();
  await flush();
  assert.ok(old.closed, "it closes its stream as soon as it runs");
  assert.equal(b.openStreams().length, 1);
  old.message({ path: P1, data: { stale: true } });
  assert.deepEqual(a.messages, [], "nothing more is relayed from the old stream");

  assert.ok(b.posts.some((p) => p.stream === "st2" && p.path === INBOX), "the new stream-holding tab follows what the woken tab follows");
  assert.deepEqual(z.statuses.slice(-3), [FeedStatus.PAUSED, FeedStatus.RESYNCED, FeedStatus.LIVE]);
  assert.equal(one.streams.length, 1, "it does not open another stream");
});

test("the stream-holding tab drops the paths of a tab that crashed", async () => {
  const b = browserProfile();
  const [one, two] = [b.open(), b.open()];
  one.follow(INBOX);
  two.follow(P1);
  await flush();
  b.hello();
  await flush();
  two.close({ crash: true });
  b.beat(2000);
  await flush();
  assert.deepEqual(b.posts.at(-1), { route: "/stream/unfollow", stream: "st1", path: P1 });
  b.beat(2000);
  await flush();
  assert.equal(b.posts.length, 3, "the living tab's path stays followed");
});

test("a tab restored from the back/forward cache follows its paths again and resyncs", async () => {
  const b = browserProfile();
  const [one, two] = [b.open(), b.open()];
  one.follow(INBOX);
  const a = two.follow(P1);
  await flush();
  b.hello();
  await flush();
  two.fire("pagehide");
  await flush();
  assert.deepEqual(b.posts.at(-1), { route: "/stream/unfollow", stream: "st1", path: P1 });
  two.fire("pageshow", { persisted: true });
  await flush();
  assert.deepEqual(b.posts.at(-1), { route: "/stream/follow", stream: "st1", path: P1 });
  assert.deepEqual(a.statuses.slice(-3), [FeedStatus.PAUSED, FeedStatus.RESYNCED, FeedStatus.LIVE]);
});

test("handovers in quick succession back off before following again", async () => {
  const b = browserProfile();
  const [one, two, three] = [b.open(), b.open(), b.open()];
  one.follow(P1);
  const a = two.follow(P1);
  const c = three.follow(P1);
  await flush();
  b.hello();
  await flush();

  one.close({ crash: true });
  await flush();
  assert.equal(two.streams.length, 0, "a handover right after the last one waits");
  assert.deepEqual(b.timers.delays(), [1000]);
  b.timers.tick();
  assert.equal(two.streams.length, 1);
  b.hello("st2");
  await flush();
  for (const f of [a, c]) assert.deepEqual(f.statuses.slice(-3), [FeedStatus.PAUSED, FeedStatus.RESYNCED, FeedStatus.LIVE]);

  two.close({ crash: true });
  await flush();
  assert.deepEqual(b.timers.delays(), [2000], "and waits twice as long the next time");
  b.timers.tick();
  b.hello("st3");
  await flush();
  assert.deepEqual(c.statuses.slice(-3), [FeedStatus.PAUSED, FeedStatus.RESYNCED, FeedStatus.LIVE]);
});
