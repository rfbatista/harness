import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { memoryFeed } from "../../../shared/infrastructure/memory-feed";
import type { ContextChanged } from "../domain/session-context";
import { httpContextFeed, projectFeedPath } from "./http-context-feed";

const setup = () => {
  const stream = memoryFeed();
  const feed = httpContextFeed(stream);
  const heard: ContextChanged[] = [];
  return { stream, feed, heard, follow: (project: string) => feed.follow(project, (c) => heard.push(c)) };
};

describe("HTTP context feed on the shared page stream", () => {
  test("follows the project feed at /events?project_id= and adds no connection of its own", () => {
    const { stream, follow } = setup();
    follow("p 1");
    assert.equal(projectFeedPath("p 1"), "/events?project_id=p%201");
    assert.equal(stream.followers("/events?project_id=p%201"), 1);
  });

  test("hears only its own project's feed", () => {
    const { stream, heard, follow } = setup();
    follow("p1");
    stream.emit(projectFeedPath("p2"), { kind: "context.changed", session_id: "s1", reason: "usage" });
    assert.deepEqual(heard, []);
  });

  test("another event with its own kind or type is passed over, and the next announcement still arrives", () => {
    const { stream, heard, follow } = setup();
    follow("p1");
    stream.emit(projectFeedPath("p1"), { kind: "task.updated", session_id: "s1", reason: "usage" });
    stream.emit(projectFeedPath("p1"), { kind: "context.changed", session_id: 7, reason: "usage" });
    stream.emit(projectFeedPath("p1"), null);
    stream.emit(projectFeedPath("p1"), { kind: "context.changed", session_id: "s1", reason: "entry_added" });
    assert.deepEqual(heard, [{ sessionId: "s1", reason: "entry_added" }]);
  });

  test("every follower hears an announcement, and one leaving does not silence the other", () => {
    const { stream, feed, follow, heard } = setup();
    const other: ContextChanged[] = [];
    const stop = feed.follow("p1", (c) => other.push(c));
    follow("p1");
    stop();
    stream.emit(projectFeedPath("p1"), { kind: "context.changed", session_id: "s1", reason: "mcp_status" });
    assert.equal(heard.length, 1);
    assert.equal(other.length, 0);
  });
});
