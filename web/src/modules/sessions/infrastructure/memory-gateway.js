// SessionGateway in memory: the fake for page tests and the data source for
// web/dev pages. It obeys the same contract as the real gateway
// (shared/testing/contracts/session-gateway.js), so tests written against it
// hold against the server.

import { Codes, StructuredError } from "../../../shared/domain/errors.js";
import { FeedStatus } from "../../../shared/domain/feed.js";
import { acceptsInput, Status } from "../domain/session.js";
import { toSeed } from "./dto.js";

/**
 * @param {{ projects?: string[], sessions?: import("../domain/session.js").Session[], now?: () => Date }} [seed]
 */
export function memoryGateway({ projects = [], sessions = [], now = () => new Date() } = {}) {
  const knownProjects = new Set([...projects, ...sessions.map((s) => s.projectId)]);
  /** @type {Map<string, import("../domain/session.js").Session>} */
  const store = new Map(sessions.map((s) => [s.id, s]));
  const followers = new Set();
  /** Messages sent per session, for assertions. */
  const sent = new Map();

  function find(sessionId) {
    const session = store.get(sessionId);
    if (!session) throw new StructuredError(Codes.SESSION_NOT_FOUND, `session ${sessionId} not found`, 404);
    return session;
  }

  function publish(projectId, change) {
    for (const f of followers) if (f.projectId === projectId) f.onChange(change);
  }

  function update(sessionId, patch) {
    const next = Object.freeze({ ...find(sessionId), ...patch, updatedAt: now() });
    store.set(sessionId, next);
    publish(next.projectId, { kind: "upsert", session: next });
    return next;
  }

  /** @type {import("../domain/ports.js").SessionGateway} */
  const gateway = {
    decodeSeed: toSeed,

    async list(projectId) {
      if (!knownProjects.has(projectId)) {
        throw new StructuredError(Codes.PROJECT_NOT_FOUND, `project ${projectId} not found`, 404);
      }
      return [...store.values()].filter((s) => s.projectId === projectId);
    },

    async send(sessionId, text) {
      const session = find(sessionId);
      if (!acceptsInput(session)) {
        throw new StructuredError(Codes.SESSION_NOT_RUNNING, `session ${sessionId} is ${session.status}`, 409);
      }
      sent.set(sessionId, [...(sent.get(sessionId) ?? []), text]);
      update(sessionId, { status: Status.RUNNING, lastAction: `message: ${text}` });
    },

    async stop(sessionId) {
      update(sessionId, { status: Status.STOPPED, pendingApprovals: 0 });
    },

    follow(projectId, onChange, onStatus) {
      const follower = { projectId, onChange, onStatus };
      followers.add(follower);
      queueMicrotask(() => followers.has(follower) && onStatus(FeedStatus.LIVE));
      return () => followers.delete(follower);
    },
  };

  return {
    gateway,
    sent,
    /** Test and dev hooks: change the world as the server would. */
    upsert(session) {
      knownProjects.add(session.projectId);
      store.set(session.id, session);
      publish(session.projectId, { kind: "upsert", session });
    },
    remove(sessionId) {
      const { projectId } = find(sessionId);
      store.delete(sessionId);
      publish(projectId, { kind: "deleted", id: sessionId });
    },
    update,
    feedStatus(status) {
      for (const f of followers) f.onStatus(status);
    },
  };
}
