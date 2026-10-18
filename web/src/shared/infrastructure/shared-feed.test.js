import { FeedStatus } from "../domain/feed.js";
import { fakeEventSourceClass, flush, manualTimers } from "../testing/doubles.js";
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
