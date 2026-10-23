import { FeedStatus } from "../../../shared/domain/feed.js";
import { assert, file, test } from "../../../shared/testing/test.js";
import { finished, missed, started } from "../testing/liveCalls.js";
import { usageGateway } from "./usage-gateway.js";

file("usage/infrastructure/usage-gateway");

/** An EventSource that records what is asked of it and lets a test play the server. */
class FakeEventSource {
  static opened = [];
  constructor(url) {
    this.url = url;
    this.closed = false;
    this.listeners = new Map();
    FakeEventSource.opened.push(this);
  }
  addEventListener(name, fn) {
    this.listeners.set(name, [...(this.listeners.get(name) ?? []), fn]);
  }
  close() {
    this.closed = true;
  }
  send(name, data) {
    for (const fn of this.listeners.get(name) ?? []) fn({ data: typeof data === "string" ? data : JSON.stringify(data) });
  }
  fail() {
    this.onerror?.();
  }
}

function open(projectId = "p 1") {
  FakeEventSource.opened = [];
  const gateway = usageGateway({}, {}, { EventSource: FakeEventSource });
  const events = [];
  const statuses = [];
  const close = gateway.openLive(projectId, { onEvent: (type, data) => events.push([type, data]), onStatus: (s) => statuses.push(s) });
  return { source: FakeEventSource.opened[0], opened: FakeEventSource.opened, events, statuses, close };
}

test("opening the live stream opens exactly one EventSource on the project's live endpoint", () => {
  const { opened, source, statuses } = open("p 1");
  assert.equal(opened.length, 1);
  assert.equal(source.url, "/api/projects/p%201/usage/live");
  assert.deepEqual(statuses, [FeedStatus.CONNECTING]);
});

test("each named event reaches the handler with its data parsed; ready makes it live", () => {
  const { source, events, statuses } = open();
  source.send("ready", { project_id: "p1" });
  source.send("call.started", started());
  source.send("call.finished", finished());
  source.send("calls.missed", missed(2));
  assert.deepEqual(events.map(([type]) => type), ["call.started", "call.finished", "calls.missed"]);
  assert.equal(events[0][1].id, "c1");
  assert.equal(events[2][1].count, 2);
  assert.deepEqual(statuses, [FeedStatus.CONNECTING, FeedStatus.LIVE]);
});

test("an event that is not JSON is dropped without breaking the stream", () => {
  const { source, events } = open();
  source.send("call.started", "{not json");
  source.send("call.started", started());
  assert.equal(events.length, 1);
});

test("a dropped stream reports paused, and ready on its own reconnect reports live again", () => {
  const { source, statuses } = open();
  source.send("ready", {});
  source.fail();
  source.send("ready", {});
  assert.deepEqual(statuses, [FeedStatus.CONNECTING, FeedStatus.LIVE, FeedStatus.PAUSED, FeedStatus.LIVE]);
});

test("closing releases the connection and nothing arrives afterwards", () => {
  const { source, events, close } = open();
  close();
  assert.equal(source.closed, true);
  source.send("call.started", started());
  assert.equal(events.length, 0);
});
