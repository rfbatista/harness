// SessionGateway in memory: the fake for page tests and the data source for
// web/dev pages. It obeys the same contract as the real gateway
// (../testing/gateway-contract.js), so tests written against it
// hold against the server.

import { Codes, StructuredError } from "../../../shared/domain/errors.js";
import { FeedStatus } from "../../../shared/domain/feed.js";
import { Status } from "../domain/session.js";
import { toSeed } from "./dto.js";

/**
 * @param {{
 *   projects?: string[],
 *   sessions?: import("../domain/session.js").Session[],
 *   repositories?: Record<string, string>,  repository id → project id
 *   branches?: Record<string, { name: string, remote: boolean, isHead: boolean }[]>,  repository id → its branches
 *   now?: () => Date,
 * }} [seed]
 */
export function memoryGateway({ projects = [], sessions = [], repositories = {}, branches = {}, now = () => new Date() } = {}) {
  let nextId = 1;
  const knownProjects = new Set([...projects, ...sessions.map((s) => s.projectId)]);
  /** @type {Map<string, import("../domain/session.js").Session>} */
  const store = new Map(sessions.map((s) => [s.id, s]));
  const followers = new Set();

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

    async list({ projectId, ticketId }) {
      if (!knownProjects.has(projectId)) {
        throw new StructuredError(Codes.PROJECT_NOT_FOUND, `project ${projectId} not found`, 404);
      }
      return [...store.values()].filter((s) => s.projectId === projectId && (!ticketId || s.ticketId === ticketId));
    },

    async start({ projectId, ticketId, repositoryId, agentId, prompt }) {
      if (!knownProjects.has(projectId)) {
        throw new StructuredError(Codes.PROJECT_NOT_FOUND, `project ${projectId} not found`, 404);
      }
      if (!repositoryId) throw new StructuredError(Codes.INVALID_INPUT, "repository_id is required", 400);
      if (repositories[repositoryId] !== undefined && repositories[repositoryId] !== projectId) {
        throw new StructuredError(Codes.CROSS_PROJECT_ACCESS, "repository does not belong to project", 400);
      }
      const session = Object.freeze({
        id: `mem-${nextId++}`,
        projectId,
        ticketId: ticketId ?? "",
        task: prompt?.trim() ?? "",
        agentId: agentId ?? "",
        status: Status.RUNNING,
        pendingApprovals: 0,
        lastAction: "",
        interactive: true,
        runsOn: "server",
        runnerHost: "",
        updatedAt: now(),
      });
      store.set(session.id, session);
      publish(projectId, { kind: "upsert", session });
      return session;
    },

    async listBranches(repositoryId) {
      const list = branches[repositoryId];
      if (!list) throw new StructuredError(Codes.REPOSITORY_NOT_FOUND, "repository not found", 404);
      return list;
    },

    async stop(sessionId) {
      update(sessionId, { status: Status.STOPPED, pendingApprovals: 0 });
    },

    async remove(sessionId) {
      const { projectId } = find(sessionId);
      store.delete(sessionId);
      publish(projectId, { kind: "deleted", id: sessionId });
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
