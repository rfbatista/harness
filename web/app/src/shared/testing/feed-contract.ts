// The contract every Feed adapter keeps: followers of a path hear its messages
// and its status, a drop pauses them and the reconnect resyncs them, and a
// follower that stopped hears nothing.

import { describe, test } from "node:test";
import assert from "node:assert/strict";
import type { Feed, FeedStatus } from "../kernel/feed";

const INBOX = "/notifications/events";

export interface FeedHarness {
  feed: Feed;
  /** The stream comes up (the inbox path becomes live). */
  connect(): Promise<void>;
  /** The server sends `data` on `path`. */
  send(path: string, data: unknown): void;
  /** The server ends the feed of `path`, so it is dropped. */
  endFeed(path: string): void;
  /** The dropped feed is followed again. */
  reconnect(): Promise<void>;
}

export function feedContract(name: string, make: () => Promise<FeedHarness>): void {
  describe(`Feed contract: ${name}`, () => {
    test("a follower is told connecting, then live, and hears the messages of its path only", async () => {
      const h = await make();
      const statuses: FeedStatus[] = [];
      const messages: unknown[] = [];
      h.feed.follow(INBOX, { onMessage: (m) => messages.push(m), onStatus: (s) => statuses.push(s) });
      await h.connect();
      h.send(INBOX, { a: 1 });
      h.send("/somewhere/else", { b: 2 });
      assert.deepEqual(statuses, ["connecting", "live"]);
      assert.deepEqual(messages, [{ a: 1 }]);
    });

    test("a dropped feed pauses its followers, and the reconnect resyncs them", async () => {
      const h = await make();
      const statuses: FeedStatus[] = [];
      h.feed.follow(INBOX, { onMessage: () => {}, onStatus: (s) => statuses.push(s) });
      await h.connect();
      h.endFeed(INBOX);
      assert.equal(statuses.at(-1), "paused");
      await h.reconnect();
      assert.deepEqual(statuses.slice(-3), ["paused", "resynced", "live"]);
    });

    test("a follower that stopped hears nothing", async () => {
      const h = await make();
      const messages: unknown[] = [];
      const stop = h.feed.follow(INBOX, { onMessage: (m) => messages.push(m) });
      await h.connect();
      stop();
      h.send(INBOX, { a: 1 });
      assert.deepEqual(messages, []);
    });
  });
}
