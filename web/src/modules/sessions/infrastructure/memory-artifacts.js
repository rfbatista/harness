// ArtifactGateway in memory: the fake for page tests and the data source for
// web/dev pages. It obeys the same contract as the real gateway
// (../testing/artifact-contract.js).

import { Codes, StructuredError } from "../../../shared/domain/errors.js";
import { FeedStatus } from "../../../shared/domain/feed.js";
import { belongsTo, byUpdated, isPromotable, SCOPES, Scope } from "../domain/artifact.js";
import { toArtifactList, viewPath } from "./artifact-dto.js";

const notFound = () => new StructuredError(Codes.ARTIFACT_NOT_FOUND, "artifact not found", 404);

/**
 * @param {{
 *   artifacts?: import("../domain/artifact.js").Artifact[],
 *   tickets?: { id: string, projectId: string }[],  tasks an asset can be attached to, besides the ones the artifacts were produced on
 *   now?: () => Date,
 * }} [world]
 */
export function memoryArtifacts({ artifacts = [], tickets = [], now = () => new Date() } = {}) {
  let nextId = 1;
  /** @type {Map<string, import("../domain/artifact.js").Artifact>} */
  const store = new Map(artifacts.map((a) => [a.id, Object.freeze({ ...a, attachedTicketIds: Object.freeze([...(a.attachedTicketIds ?? [])]) })]));
  /** Ticket id → its project. */
  const projects = new Map([...artifacts.map((a) => [a.ticketId, a.projectId]), ...tickets.map((t) => [t.id, t.projectId])]);
  const followers = new Set();
  const projectFollowers = new Set();

  function notify(sessionId, event) {
    for (const f of followers) if (f.sessionId === sessionId) f.onEvent(event);
  }

  function notifyProject(projectId, change) {
    for (const f of projectFollowers) if (f.projectId === projectId) f.onChange(change);
  }

  /** Stores a change and puts it on the project feed. */
  function save(artifact) {
    store.set(artifact.id, artifact);
    notifyProject(artifact.projectId, { kind: "changed", artifact });
    return artifact;
  }

  function withAttached(a, ids) {
    return Object.freeze({ ...a, attachedTicketIds: Object.freeze(ids) });
  }

  function requireIds(artifactId, ticketId) {
    if (!artifactId || !ticketId) throw new StructuredError(Codes.INVALID_INPUT, "artifact_id and ticket_id are required", 400);
    const a = store.get(artifactId);
    if (!a) throw notFound();
    return a;
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
    decodeArtifacts(rows) {
      return toArtifactList({ artifacts: rows });
    },
    async listTask(ticketId) {
      return byUpdated([...store.values()].filter((a) => belongsTo(a, ticketId)));
    },
    async setScope(artifactId, scope) {
      if (!SCOPES.includes(scope)) throw new StructuredError(Codes.INVALID_INPUT, "scope must be task or project", 400);
      const a = store.get(artifactId);
      if (!a) throw notFound();
      if (a.scope === scope) return a;
      if (scope === Scope.PROJECT && !isPromotable(a)) {
        throw new StructuredError(Codes.ARTIFACT_NOT_PROMOTABLE, "a url artifact cannot move to the project: a dev server dies with its session", 409);
      }
      // A move is a change, never a revision; the clock may not have moved, the record must.
      const at = new Date(Math.max(now().getTime(), a.updatedAt.getTime() + 1));
      // Back with its task, it is no other task's any more.
      const moved = Object.freeze({ ...a, scope, updatedAt: at, attachedTicketIds: Object.freeze(scope === Scope.TASK ? [] : [...a.attachedTicketIds]) });
      save(moved);
      notify(a.sessionId, { kind: "published", artifact: moved });
      return moved;
    },
    async remove(artifactId) {
      const a = store.get(artifactId);
      if (!a) throw notFound();
      store.delete(artifactId);
      notifyProject(a.projectId, { kind: "deleted", id: a.id, projectId: a.projectId, ticketId: a.ticketId, attachedTicketIds: [...a.attachedTicketIds] });
    },
    async attach(artifactId, ticketId) {
      const a = requireIds(artifactId, ticketId);
      if (!projects.has(ticketId)) throw new StructuredError(Codes.TICKET_NOT_FOUND, "ticket not found", 404);
      if (ticketId === a.ticketId || a.attachedTicketIds.includes(ticketId)) return a;
      if (projects.get(ticketId) !== a.projectId) {
        throw new StructuredError(Codes.ARTIFACT_PROJECT_MISMATCH, "the task belongs to another project", 409);
      }
      if (a.scope !== Scope.PROJECT) {
        throw new StructuredError(Codes.ARTIFACT_NOT_IN_PROJECT, "only a project asset can be attached; move it to the project first", 409);
      }
      return save(withAttached(a, [...a.attachedTicketIds, ticketId]));
    },
    async detach(artifactId, ticketId) {
      const a = requireIds(artifactId, ticketId);
      if (ticketId === a.ticketId) throw new StructuredError(Codes.ARTIFACT_PRODUCER_TASK, "the producing task cannot be detached", 409);
      if (!a.attachedTicketIds.includes(ticketId)) return a;
      return save(withAttached(a, a.attachedTicketIds.filter((id) => id !== ticketId)));
    },
    follow(sessionId, onEvent, onStatus) {
      const follower = { sessionId, onEvent, onStatus };
      followers.add(follower);
      queueMicrotask(() => followers.has(follower) && onStatus(FeedStatus.LIVE));
      return () => followers.delete(follower);
    },
    followProject(projectId, onChange, onStatus) {
      const follower = { projectId, onChange, onStatus };
      projectFollowers.add(follower);
      queueMicrotask(() => projectFollowers.has(follower) && onStatus(FeedStatus.LIVE));
      return () => projectFollowers.delete(follower);
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
      if (!projects.has(ticketId)) projects.set(ticketId, projectId);
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
        attachedTicketIds: Object.freeze([...(earlier?.attachedTicketIds ?? [])]),
        createdAt: earlier?.createdAt ?? at,
        updatedAt: at,
        src: kind === "url" ? url : viewPath(id),
      });
      // A re-publish of a project asset is news to every page of the project.
      if (artifact.scope === Scope.PROJECT) save(artifact);
      else store.set(id, artifact);
      notify(sessionId, { kind: "published", artifact });
      return artifact;
    },
    /** A task is deleted: its attachments go, the assets stay in the project. */
    deleteTask(ticketId) {
      projects.delete(ticketId);
      for (const a of [...store.values()]) {
        if (a.attachedTicketIds.includes(ticketId)) save(withAttached(a, a.attachedTicketIds.filter((id) => id !== ticketId)));
      }
    },
    /** The session ends: followers hear it once. */
    end(sessionId) {
      notify(sessionId, { kind: "ended" });
    },
    feedStatus(status) {
      for (const f of followers) f.onStatus(status);
      for (const f of projectFollowers) f.onStatus(status);
    },
  };
}
