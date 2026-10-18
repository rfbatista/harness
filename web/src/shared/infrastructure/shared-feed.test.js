import { FeedStatus } from "../domain/feed.js";
import { fakeCrossTab, fakeEventSourceClass, flush, manualTimers } from "../testing/doubles.js";
import { assert, file, test } from "../testing/test.js";
import { sharedFeed } from "./shared-feed.js";

file("shared/infrastructure/shared-feed");

// An api whose posts are recorded; a follow fails while `refuse` says so.
function fakeApi() {
  const posts = [];
  const api = {
    refuse: false,
    post(path, body) {
      posts.push({ route: path, ...body });
      return api.refuse ? Promise.reject(new Error("refused")) : Promise.resolve({});
    },
  };
  return { api, posts };
}

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
    const { api, posts } = fakeApi();
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
  const { api, posts } = fakeApi();
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
    const f = sharedFeed({ base: "/api", api, EventSource: TabEventSource, ...timers, locks: t.locks, BroadcastChannel: t.BroadcastChannel, window: t.window });
    const tab = {
      feed: f,
      follow: (path) => followerOn(f, path),
      close(opts) {
        t.close(opts);
        for (const s of mine.instances) s.closed = true;
      },
      streams: mine.instances,
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
  return { open, tabs, es, timers, api, posts, openStreams, stream, hello };
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
