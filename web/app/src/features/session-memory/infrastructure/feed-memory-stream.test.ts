import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { memoryFeed } from "../../../shared/infrastructure/memory-feed";
import type { MemoryKill, MemorySample } from "../../../shared/kernel/memory/sample";
import { wireKill, wireSample } from "../../../shared/testing/memory-fixtures";
import { feedMemoryStream } from "./feed-memory-stream";

const PATH = "/sessions/s1/events";

function follow() {
  const feed = memoryFeed();
  const got = { samples: [] as MemorySample[], kills: [] as MemoryKill[], resyncs: 0 };
  const stop = feedMemoryStream(feed).follow("s1", { onSample: (s) => got.samples.push(s), onKill: (k) => got.kills.push(k), onResync: () => got.resyncs++ });
  return { feed, got, stop };
}

describe("memory stream over the session's own feed", () => {
  test("follows the session's events and decodes the two memory kinds, ignoring everything else", () => {
    const { feed, got } = follow();
    assert.equal(feed.followers(PATH), 1);
    feed.emit(PATH, { seq: 4, type: "session.memory", ...wireSample() });
    feed.emit(PATH, { seq: 5, type: "session.memory_killed", ...wireKill() });
    feed.emit(PATH, { seq: 6, type: "message", text: "hello" });
    feed.emit(PATH, "not an object");
    assert.deepEqual([got.samples.length, got.kills.length], [1, 1]);
    assert.equal(got.samples[0]?.rssBytes, 1288490188);
    assert.equal(got.kills[0]?.signal, "SIGTERM");
  });

  test("a malformed memory event is dropped, not thrown", () => {
    const { feed, got } = follow();
    feed.emit(PATH, wireSample({ rss_bytes: "lots" }));
    assert.equal(got.samples.length, 0);
  });

  test("a stream that came back after a drop reports a resync, and nothing else does", () => {
    const { feed, got } = follow();
    feed.setStatus(PATH, "live");
    feed.setStatus(PATH, "paused");
    assert.equal(got.resyncs, 0);
    feed.setStatus(PATH, "resynced");
    assert.equal(got.resyncs, 1);
  });

  test("stopping unfollows", () => {
    const { feed, stop } = follow();
    stop();
    assert.equal(feed.followers(PATH), 0);
  });
});
