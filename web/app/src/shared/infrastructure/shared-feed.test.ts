// The shared page event stream protocol (ADR 0011), as this client speaks it and
// as the Alpine client's shared-feed.js does, in one simulated browser profile.

import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { browserProfile, flush, type Client } from "../testing/browser-profile";
import { feedContract } from "../testing/feed-contract";
import { NAME, sharedFeed } from "./shared-feed";
import { memoryFeed } from "./memory-feed";

const P1 = "/events?project_id=p1";
const INBOX = "/notifications/events";

feedContract("shared-stream adapter in one tab", async () => {
  const b = browserProfile();
  const tab = b.open();
  return {
    feed: tab.feed,
    async connect() {
      await flush();
      b.hello();
      await flush();
    },
    send: (path, data) => b.openStreams()[0]?.message({ path, data }),
    endFeed: (path) => b.stream().message({ path, end: true }),
    async reconnect() {
      b.timers.tick();
      await flush();
    },
    sendMalformed: () => b.openStreams()[0]?.message("{not json"),
  };
});

feedContract("in-memory feed", async () => {
  const feed = memoryFeed();
  return {
    feed,
    connect: async () => feed.setStatus(INBOX, "live"),
    send: (path, data) => feed.emit(path, data),
    endFeed: (path) => feed.setStatus(path, "paused"),
    reconnect: async () => {
      feed.setStatus(INBOX, "resynced");
      feed.setStatus(INBOX, "live");
    },
  };
});

describe("shared-stream protocol", () => {
  test("uses the Alpine client's lock and channel name", () => {
    assert.equal(NAME, "harness-page-event-stream");
  });

  test("without the cross-tab APIs a tab keeps its own stream, as before", async () => {
    for (const missing of [{ locks: null }, { BroadcastChannel: null }]) {
      const b = browserProfile();
      const f = sharedFeed({ base: "/api", api: b.api, EventSource: b.es.EventSource, ...b.timers, ...missing } as never);
      const statuses: string[] = [];
      f.follow(P1, { onMessage: () => {}, onStatus: (s) => statuses.push(s) });
      assert.equal(b.es.instances.length, 1);
      b.stream().message({ stream: "st1" });
      await flush();
      assert.deepEqual(statuses, ["connecting", "live"]);
    }
  });

  test("a stream message and a channel message that are not the protocol's are ignored, and the follows go on", async () => {
    const b = browserProfile();
    const [one, two] = [b.open(), b.open()];
    const a = one.follow(INBOX);
    const c = two.follow(INBOX);
    await flush();
    b.hello();
    await flush();
    for (const garbage of ["not json", "42", "[]", "{}", '{"path":7}', '{"stream":""}']) b.stream().message(garbage);
    for (const bad of [null, "ping", 3, {}, { type: "teleport", from: "x" }, { type: "follow", from: "x" }, { type: "status", from: "x", path: INBOX, status: "asleep" }]) b.broadcast(bad);
    b.stream().message({ path: INBOX, data: { n: 1 } });
    assert.deepEqual(a.messages, [{ n: 1 }]);
    assert.deepEqual(c.messages, [{ n: 1 }]);
    assert.deepEqual(a.statuses, ["connecting", "live"]);
  });

  for (const [name, clients] of [
    ["React tabs only", ["react", "react", "react"]],
    ["React tabs among Alpine tabs", ["alpine", "react", "alpine"]],
    ["an Alpine tab holding, React tabs following", ["alpine", "react", "react"]],
    ["a React tab holding, Alpine tabs following", ["react", "alpine", "alpine"]],
  ] as [string, Client[]][]) {
    describe(name, () => {
      test("however many tabs follow, one stream is open, in the stream-holding tab", async () => {
        const b = browserProfile();
        const tabs = clients.map((c) => b.open(c));
        tabs.forEach((t) => t.follow(P1));
        await flush();
        assert.equal(b.es.instances.length, 1);
        assert.equal(tabs[0]!.streams.length, 1, "the first tab holds the stream");
        assert.equal(b.stream().url, "/api/stream");
      });

      test("a path several tabs follow is followed once; an event reaches the followers of its path in every tab, and no one else", async () => {
        const b = browserProfile();
        const [one, two, three] = clients.map((c) => b.open(c)) as [ReturnType<typeof b.open>, ReturnType<typeof b.open>, ReturnType<typeof b.open>];
        const a = one.follow(P1);
        const c = two.follow(P1);
        const d = three.follow(INBOX);
        await flush();
        b.hello();
        await flush();
        assert.deepEqual(b.posts, [
          { route: "/stream/follow", stream: "st1", path: P1 },
          { route: "/stream/follow", stream: "st1", path: INBOX },
        ]);
        b.stream().message({ path: P1, data: { ticket: { id: "t1" } } });
        b.stream().message({ path: INBOX, data: { changed: [], unread_count: 1 } });
        assert.deepEqual(a.messages, [{ ticket: { id: "t1" } }]);
        assert.deepEqual(c.messages, [{ ticket: { id: "t1" } }]);
        assert.deepEqual(d.messages, [{ changed: [], unread_count: 1 }]);
        assert.deepEqual(d.statuses, ["connecting", "live"]);
      });

      test("when the stream-holding tab closes, another takes the stream over and every tab resyncs", async () => {
        const b = browserProfile();
        const [one, two, three] = clients.map((c) => b.open(c)) as [ReturnType<typeof b.open>, ReturnType<typeof b.open>, ReturnType<typeof b.open>];
        one.follow(INBOX);
        const a = two.follow(INBOX);
        const c = three.follow(INBOX);
        await flush();
        b.hello();
        await flush();
        b.later(60_000); // a quiet spell: the handover is at once
        one.close();
        await flush();
        for (const f of [a, c]) assert.equal(f.statuses.at(-1), "paused");
        assert.equal(b.openStreams().length, 1, "one stream again, in one tab");
        b.hello("st2");
        await flush();
        for (const f of [a, c]) assert.deepEqual(f.statuses.slice(-3), ["paused", "resynced", "live"]);
        b.stream().message({ path: INBOX, data: { n: 1 } });
        assert.deepEqual(a.messages, [{ n: 1 }]);
        assert.deepEqual(c.messages, [{ n: 1 }]);
      });

      test("a frozen stream-holding tab is replaced by exactly one tab after ten seconds of silence", async () => {
        const b = browserProfile();
        const [one, two, three] = clients.map((c) => b.open(c)) as [ReturnType<typeof b.open>, ReturnType<typeof b.open>, ReturnType<typeof b.open>];
        one.follow(INBOX);
        const a = two.follow(INBOX);
        three.follow(INBOX);
        await flush();
        b.hello();
        await flush();
        b.later(60_000);
        one.freeze();
        for (let s = 0; s < 8; s += 2) b.beat(2000);
        assert.equal(two.streams.length + three.streams.length, 0, "eight seconds of silence is not enough");
        for (let s = 0; s < 4; s += 2) b.beat(2000);
        await flush();
        const taken = [two, three].filter((t) => t.streams.length > 0);
        assert.equal(taken.length, 1, "exactly one tab takes the stream over");
        taken[0]!.streams[0]!.message({ stream: "st2" });
        await flush();
        assert.deepEqual(a.statuses.slice(-3), ["paused", "resynced", "live"]);
      });
    });
  }
});
