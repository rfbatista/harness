import { memoryFeed } from "../../../shared/infrastructure/memory-feed";
import { contextFeedContract } from "../testing/context-feed-contract";
import { httpContextFeed, projectFeedPath } from "./http-context-feed";
import { memoryContextFeed } from "./memory-context-feed";

contextFeedContract("shared page stream adapter", () => {
  const stream = memoryFeed();
  return {
    feed: httpContextFeed(stream),
    announce: (projectId, data) => stream.emit(projectFeedPath(projectId), data),
    followers: (projectId) => stream.followers(projectFeedPath(projectId)),
  };
});

contextFeedContract("in-memory adapter", () => {
  const feed = memoryContextFeed();
  return {
    feed,
    announce: (_projectId, data) => {
      // The memory feed carries only valid changes; the stream's filtering is the adapter above.
      const d = data as { kind?: unknown; session_id?: unknown; reason?: unknown };
      if (d?.kind === "context.changed" && typeof d?.session_id === "string" && (d.reason === "usage" || d.reason === "entry_added" || d.reason === "mcp_status")) feed.emit({ sessionId: d.session_id, reason: d.reason });
    },
    followers: (projectId) => feed.followers(projectId),
  };
});
