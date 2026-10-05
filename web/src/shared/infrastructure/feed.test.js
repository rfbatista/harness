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
