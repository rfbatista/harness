// ArtifactGateway in memory: the fake for page tests and the data source for
// web/dev pages. It obeys the same contract as the real gateway
// (../testing/artifact-contract.js).

import { Codes, StructuredError } from "../../../shared/domain/errors.js";
import { FeedStatus } from "../../../shared/domain/feed.js";
import { byUpdated, isPromotable, SCOPES, Scope } from "../domain/artifact.js";
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
    async listProject(projectId) {
      return byUpdated([...store.values()].filter((a) => a.projectId === projectId && a.scope === Scope.PROJECT));
    },
    async setScope(artifactId, scope) {
      if (!SCOPES.includes(scope)) throw new StructuredError(Codes.INVALID_INPUT, "scope must be task or project", 400);
      const a = store.get(artifactId);
      if (!a) throw new StructuredError(Codes.ARTIFACT_NOT_FOUND, "artifact not found", 404);
      if (a.scope === scope) return a;
      if (scope === Scope.PROJECT && !isPromotable(a)) {
        throw new StructuredError(Codes.ARTIFACT_NOT_PROMOTABLE, "a url artifact cannot move to the project: a dev server dies with its session", 409);
      }
      // A move is a change, never a revision; the clock may not have moved, the record must.
      const at = new Date(Math.max(now().getTime(), a.updatedAt.getTime() + 1));
      const moved = Object.freeze({ ...a, scope, updatedAt: at });
      store.set(a.id, moved);
      notify(a.sessionId, { kind: "published", artifact: moved });
      return moved;
    },
    async remove(artifactId) {
      if (!store.delete(artifactId)) throw new StructuredError(Codes.ARTIFACT_NOT_FOUND, "artifact not found", 404);
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
        scope: earlier?.scope ?? Scope.TASK,
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
