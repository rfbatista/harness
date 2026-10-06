// ArtifactGateway in memory: the fake for page tests and the data source for
// web/dev pages. It obeys the same contract as the real gateway
// (../testing/artifact-contract.js).

import { FeedStatus } from "../../../shared/domain/feed.js";
import { byUpdated } from "../domain/artifact.js";
import { viewPath } from "./artifact-dto.js";

/**
 * @param {{
 *   artifacts?: import("../domain/artifact.js").Artifact[],
 *   now?: () => Date,
 * }} [world]
 */
export function memoryArtifacts({ artifacts = [], now = () => new Date() } = {}) {
  let nextId = 1;
  /** @type {Map<string, import("../domain/artifact.js").Artifact>} */
  const store = new Map(artifacts.map((a) => [a.id, a]));
  const followers = new Set();

  function notify(sessionId, event) {
    for (const f of followers) if (f.sessionId === sessionId) f.onEvent(event);
  }

  /** One artifact per (session, path) or (session, url). */
  const identity = (a) => `${a.sessionId}\n${a.kind === "url" ? a.url : a.path}`;

  /** @type {import("../domain/ports.js").ArtifactGateway} */
  const gateway = {
    async list(sessionId) {
      return byUpdated([...store.values()].filter((a) => a.sessionId === sessionId));
    },
    follow(sessionId, onEvent, onStatus) {
      const follower = { sessionId, onEvent, onStatus };
      followers.add(follower);
      queueMicrotask(() => followers.has(follower) && onStatus(FeedStatus.LIVE));
      return () => followers.delete(follower);
    },
  };

  return {
    gateway,
    /** Publishes as a session's publish_artifact tool would: a new artifact, or a new revision of the one at the same path/url. */
    publish({ sessionId, kind, title = "", note = "", path = "", url = "", mime = "", sizeBytes = 0, ticketId = "t1", projectId = "p1" }) {
      const key = `${sessionId}\n${kind === "url" ? url : path}`;
      const earlier = [...store.values()].find((a) => identity(a) === key);
      const at = now();
      const id = earlier?.id ?? `art-${nextId++}`;
      const artifact = Object.freeze({
        id,
        sessionId,
        ticketId,
        projectId,
        kind,
        title,
        note,
        path: kind === "url" ? "" : path,
        url: kind === "url" ? url : "",
        mime,
        sizeBytes,
        revision: (earlier?.revision ?? 0) + 1,
        createdAt: earlier?.createdAt ?? at,
        updatedAt: at,
        src: kind === "url" ? url : viewPath(id),
      });
      store.set(id, artifact);
      notify(sessionId, { kind: "published", artifact });
      return artifact;
    },
    /** The session ends: followers hear it once. */
    end(sessionId) {
      notify(sessionId, { kind: "ended" });
    },
    feedStatus(status) {
      for (const f of followers) f.onStatus(status);
    },
  };
}
