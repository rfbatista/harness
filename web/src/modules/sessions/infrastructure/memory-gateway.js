// SessionGateway in memory: the fake for page tests and the data source for
// web/dev pages. It obeys the same contract as the real gateway
// (../testing/gateway-contract.js), so tests written against it
// hold against the server.

import { Codes, StructuredError } from "../../../shared/domain/errors.js";
import { FeedStatus } from "../../../shared/domain/feed.js";
import { isTerminal, MODES, noTaskProblem, Status } from "../domain/session.js";
import { toSeed } from "./dto.js";

/** The first rule of git check-ref-format a name breaks, or "": enough for pages and tests. */
function invalidReason(name) {
  if (/\s/.test(name)) return "a branch name cannot contain spaces";
  if (name.includes("..")) return "a branch name cannot contain ..";
  if (/[~^:?*[\\]/.test(name)) return "a branch name cannot contain ~ ^ : ? * [ or a backslash";
  if (/^[/.-]|[/.]$|\/\/|\.lock$/.test(name)) return "a branch name cannot start with / . or -, end with / or ., contain //, or end in .lock";
  return "";
}

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

  const taken = (repositoryId) => new Set((branches[repositoryId] ?? []).filter((b) => !b.remote).map((b) => b.name));

  /** The name the server would generate: agent/<task>-<suffix>. */
  const generatedName = (ticketId) => `agent/${ticketId || "session"}-${(nextId + 0x1000).toString(16)}`;

  function check({ repositoryId, name = "", ticketId = "" }) {
    if (!(repositoryId in branches) && !(repositoryId in repositories)) {
      throw new StructuredError(Codes.REPOSITORY_NOT_FOUND, "repository not found", 404);
    }
    const trimmed = name.trim();
    const generated = generatedName(ticketId);
    const base = { name: trimmed, generated, valid: true, available: true, code: "", reason: "", suggestion: "" };
    if (!trimmed) return base;
    const reason = invalidReason(trimmed);
    if (reason) return { ...base, valid: false, available: false, code: Codes.BRANCH_NAME_INVALID, reason };
    const existing = taken(repositoryId);
    if (!existing.has(trimmed)) return base;
    let n = 2;
    while (existing.has(`${trimmed}-${n}`)) n++;
    return { ...base, available: false, code: Codes.BRANCH_EXISTS, reason: `${trimmed} already exists`, suggestion: `${trimmed}-${n}` };
  }

  function find(sessionId) {
    const session = store.get(sessionId);
    if (!session) throw new StructuredError(Codes.SESSION_NOT_FOUND, `session ${sessionId} not found`, 404);
    return session;
  }

  function refuse(code, message) {
    throw new StructuredError(code, message, 409);
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

    async checkBranchName(query) {
      return Object.freeze(check(query));
    },

    async start({ projectId, ticketId, repositoryId, agentId, prompt, mode, branchName }) {
      if (!knownProjects.has(projectId)) {
        throw new StructuredError(Codes.PROJECT_NOT_FOUND, `project ${projectId} not found`, 404);
      }
      if (!repositoryId) throw new StructuredError(Codes.INVALID_INPUT, "repository_id is required", 400);
      if (repositories[repositoryId] !== undefined && repositories[repositoryId] !== projectId) {
        throw new StructuredError(Codes.CROSS_PROJECT_ACCESS, "repository does not belong to project", 400);
      }
      if (!MODES.includes(mode ?? "")) throw new StructuredError(Codes.INVALID_INPUT, `unknown mode "${mode}"`, 400);
      if (!ticketId && noTaskProblem(mode)) throw new StructuredError(Codes.INVALID_INPUT, "ticket_id is required unless mode is product", 400);
      if (branchName?.trim()) {
        const verdict = check({ repositoryId, name: branchName });
        if (verdict.code === Codes.BRANCH_NAME_INVALID) {
          throw new StructuredError(Codes.BRANCH_NAME_INVALID, `BRANCH_NAME_INVALID: ${verdict.reason}`, 400, { name: verdict.name, reason: verdict.reason });
        }
        if (verdict.code === Codes.BRANCH_EXISTS) {
          throw new StructuredError(Codes.BRANCH_EXISTS, `BRANCH_EXISTS: ${verdict.name} already exists on this repository`, 409, { name: verdict.name, suggestion: verdict.suggestion });
        }
      }
      const session = Object.freeze({
        id: `mem-${nextId++}`,
        projectId,
        ticketId: ticketId ?? "",
        task: prompt?.trim() ?? "",
        agentId: agentId ?? "",
        mode: mode ?? "",
        status: Status.RUNNING,
        pendingApprovals: 0,
        lastAction: "",
        interactive: true,
        runsOn: "server",
        runnerHost: "",
        resumable: false,
        resumeBlocked: Codes.SESSION_ALREADY_RUNNING,
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

    async resume(sessionId) {
      // Checked and updated in one step, so a second resume finds it running.
      const session = find(sessionId);
      if (!session.interactive) refuse(Codes.SESSION_NOT_INTERACTIVE, "a headless session has nothing to resume");
      if (!isTerminal(session)) refuse(Codes.SESSION_ALREADY_RUNNING, `session ${sessionId} is already running`);
      if (!session.resumable) refuse(session.resumeBlocked || Codes.SESSION_ALREADY_RUNNING, `session ${sessionId} cannot be resumed`);
      return update(sessionId, {
        status: Status.RUNNING,
        runsOn: "server",
        runnerHost: "",
        resumable: false,
        resumeBlocked: Codes.SESSION_ALREADY_RUNNING,
      });
    },

    async stop(sessionId) {
      const { interactive } = find(sessionId);
      update(sessionId, {
        status: Status.STOPPED,
        pendingApprovals: 0,
        resumable: interactive,
        resumeBlocked: interactive ? "" : Codes.SESSION_NOT_INTERACTIVE,
      });
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
