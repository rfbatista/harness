// The contract every ContextFeed adapter keeps (Contract: Harness server ↔ Web
// UI — session context, C5). Run against the shared-stream adapter and the
// in-memory adapter.

import { describe, test } from "node:test";
import assert from "node:assert/strict";
import type { ContextFeed } from "../application/ports";
import type { ContextChanged } from "../domain/session-context";

export interface ContextFeedHarness {
  feed: ContextFeed;
  /** The server announces `context.changed` with this data, as the stream delivers it. */
  announce(projectId: string, data: unknown): void;
  /** How many followers the project has on the stream. */
  followers(projectId: string): number;
}

export function contextFeedContract(name: string, make: () => ContextFeedHarness): void {
  describe(`ContextFeed contract: ${name}`, () => {
    test("announces each change with its session and reason", () => {
      const h = make();
      const heard: ContextChanged[] = [];
      h.feed.follow("p1", (c) => heard.push(c));
      h.announce("p1", { session_id: "s1", reason: "usage" });
      h.announce("p1", { session_id: "s2", reason: "entry_added" });
      assert.deepEqual(heard, [
        { sessionId: "s1", reason: "usage" },
        { sessionId: "s2", reason: "entry_added" },
      ]);
    });

    test("ignores the project feed's other events and malformed ones", () => {
      const h = make();
      const heard: ContextChanged[] = [];
      h.feed.follow("p1", (c) => heard.push(c));
      h.announce("p1", { type: "task_updated", task_id: "t1" });
      h.announce("p1", { session_id: "s1", reason: "something_new" });
      h.announce("p1", "text");
      assert.deepEqual(heard, []);
    });

    test("stops announcing after unsubscribe", () => {
      const h = make();
      const heard: ContextChanged[] = [];
      const stop = h.feed.follow("p1", (c) => heard.push(c));
      assert.equal(h.followers("p1"), 1);
      stop();
      assert.equal(h.followers("p1"), 0);
      h.announce("p1", { session_id: "s1", reason: "usage" });
      assert.deepEqual(heard, []);
    });
  });
}
