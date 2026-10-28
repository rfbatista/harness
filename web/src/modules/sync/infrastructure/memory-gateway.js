// SyncGateway in memory: the fake for component tests and the data source for
// web/dev pages. It obeys the same contract as the real gateway
// (../testing/gateway-contract.js), and lets a test or the dev console play
// the rest of the world: the remote moving, another browser pushing, the code
// watcher re-reading a worktree, a slow fetch, a feed echo that beats its POST.

import { Codes, StructuredError } from "../../../shared/domain/errors.js";
import { FeedStatus } from "../../../shared/domain/feed.js";
import { upstreamStateOf } from "../domain/gitState.js";
import { toSeed } from "./dto.js";

/** The contract's HTTP status per code, so the stub API answers as the server would. */
export const STATUS = Object.freeze({
  [Codes.INVALID_INPUT]: 400,
  [Codes.WORKSPACE_NOT_FOUND]: 404,
  [Codes.REPOSITORY_NOT_FOUND]: 404,
  [Codes.NO_REMOTE]: 400,
  [Codes.GIT_CREDENTIAL_MISSING]: 400,
  [Codes.GIT_CREDENTIAL_REJECTED]: 502,
  [Codes.GIT_CREDENTIAL_STORE_LOCKED]: 503,
  [Codes.FETCH_FAILED]: 502,
  [Codes.PUSH_REJECTED]: 409,
  [Codes.PUSH_STALE]: 409,
  [Codes.PUSH_FAILED]: 502,
  [Codes.WORKSPACE_MISSING]: 409,
  [Codes.UNAVAILABLE]: 503,
});

const REASONS = {
  [Codes.NO_REMOTE]: "the repository has no origin remote",
  [Codes.GIT_CREDENTIAL_MISSING]: "no stored credential resolves for the origin",
  [Codes.GIT_CREDENTIAL_REJECTED]: "Bad credentials",
  [Codes.FETCH_FAILED]: "fatal: unable to access 'https://github.com/acme/harness.git/': Could not resolve host: github.com",
  [Codes.PUSH_REJECTED]: "! [rejected] (non-fast-forward)",
  [Codes.PUSH_STALE]: "the branch is no longer at the commit you saw",
  [Codes.PUSH_FAILED]: "error: failed to push some refs",
  [Codes.WORKSPACE_MISSING]: "the worktree is gone",
};

const refusal = (code, reason = REASONS[code] ?? "refused", details) => new StructuredError(code, `${code}: ${reason}`, STATUS[code] ?? 500, details);

/**
 * What the world holds of one workspace: its state as the server read it, the
 * project it belongs to, and what a fetch would bring.
 * @typedef {object} MemoryWorkspace
 * @property {import("../domain/gitState.js").GitState} state
 * @property {string} [projectId]
 * @property {string} [taskId]
 * @property {{ ahead: number, behind: number } | null} [remoteAfterFetch]  the upstream's counts once fetched (default: unchanged)
 * @property {{ host: string, kind: string } | null} [credentialMissing]    fetch and push fail with GIT_CREDENTIAL_MISSING
 * @property {boolean} [noRemote]
 */

/**
 * @param {{
 *   workspaces?: MemoryWorkspace[],
 *   now?: () => Date,
 *   delayMs?: number,      how long a fetch or a push takes
 *   echoFirst?: boolean,   whether the feed echo of a fetch or a push lands before its answer
 * }} [world]
 */
export function memorySync({ workspaces = [], now = () => new Date(), delayMs = 0, echoFirst = false } = {}) {
  /** @type {Map<string, Required<MemoryWorkspace>>} */
  const spaces = new Map();
  const followers = new Set();
  let failure = null;
  let delay = delayMs;
  let echoesFirst = echoFirst;
  const calls = { fetch: 0, push: 0, get: 0 };
  const pushed = [];

  for (const w of workspaces) addWorkspace(w);

  function addWorkspace(w) {
    spaces.set(w.state.workspaceId, { projectId: "p1", taskId: "t1", remoteAfterFetch: null, credentialMissing: null, noRemote: false, ...w });
  }

  function workspace(id) {
    const w = spaces.get(id);
    if (!w) throw new StructuredError(Codes.WORKSPACE_NOT_FOUND, `WORKSPACE_NOT_FOUND: workspace ${id} not found`, 404);
    return w;
  }

  const macrotask = () => new Promise((resolve) => setTimeout(resolve, 0));
  const wait = () => (delay > 0 ? new Promise((resolve) => setTimeout(resolve, delay)) : Promise.resolve());

  /** Reads a new state of the workspace, as the server's watcher or an action would. */
  function reread(w, fields) {
    w.state = Object.freeze({ ...w.state, ...fields, readAt: now() });
    return w.state;
  }

  /** Tells the project's followers a state, as the server's feed would. */
  function announce(w) {
    for (const f of [...followers]) if (f.projectId === w.projectId) f.onState(w.state);
    return w.state;
  }

  function takeFailure() {
    const f = failure;
    failure = null;
    return f;
  }

  /** @type {import("../domain/ports.js").SyncGateway} */
  const gateway = {
    async get(workspaceId) {
      calls.get++;
      return workspace(workspaceId).state;
    },

    async listForTask(taskId) {
      if (!taskId) throw new StructuredError(Codes.INVALID_INPUT, "INVALID_INPUT: task_id is required", 400);
      return [...spaces.values()].filter((w) => w.taskId === taskId).map((w) => w.state);
    },

    async fetchRemote(repositoryId) {
      calls.fetch++;
      const mine = [...spaces.values()].filter((w) => w.state.repositoryId === repositoryId);
      if (mine.length === 0) throw new StructuredError(Codes.REPOSITORY_NOT_FOUND, `REPOSITORY_NOT_FOUND: repository ${repositoryId} not found`, 404);
      await wait();
      const f = takeFailure();
      if (f) throw refusal(f.code, f.reason, f.details);
      if (mine.some((w) => w.noRemote)) throw refusal(Codes.NO_REMOTE);
      const missing = mine.find((w) => w.credentialMissing);
      if (missing) throw refusal(Codes.GIT_CREDENTIAL_MISSING, undefined, missing.credentialMissing);
      const fetchedAt = now();
      for (const w of mine) {
        const up = w.state.upstream;
        const remote = w.remoteAfterFetch;
        const ref = up.ref;
        const ahead = remote ? remote.ahead : (up.ahead ?? 0);
        const behind = remote ? remote.behind : (up.behind ?? 0);
        const state = upstreamStateOf({ ref, ahead, behind, fetchedAt, pushedAt: w.state.pushedAt, exists: up.state !== "gone" });
        const counted = state !== "none" && state !== "gone" && state !== "unknown";
        reread(w, { fetchedAt, upstream: Object.freeze({ ...up, state, ahead: counted ? ahead : null, behind: counted ? behind : null }) });
      }
      if (echoesFirst) {
        for (const w of mine) announce(w);
        await macrotask();
      } else {
        setTimeout(() => mine.forEach(announce), 0);
      }
      return { fetchedAt };
    },

    async push({ workspaceId, expectedHead }) {
      calls.push++;
      if (!workspaceId) throw new StructuredError(Codes.INVALID_INPUT, "INVALID_INPUT: workspace_id is required", 400);
      const w = workspace(workspaceId);
      await wait();
      const f = takeFailure();
      if (f) throw refusal(f.code, f.reason, f.details);
      if (w.state.gone) throw refusal(Codes.WORKSPACE_MISSING);
      if (w.noRemote) throw refusal(Codes.NO_REMOTE);
      if (w.credentialMissing) throw refusal(Codes.GIT_CREDENTIAL_MISSING, undefined, w.credentialMissing);
      if (expectedHead !== w.state.head) throw refusal(Codes.PUSH_STALE, undefined, { state: reread(w, {}) });
      const up = w.state.upstream;
      if (up.state === "behind" || up.state === "diverged") throw refusal(Codes.PUSH_REJECTED, undefined, { state: reread(w, {}) });
      pushed.push({ workspaceId, expectedHead });
      const ref = up.ref ?? `origin/${w.state.branch}`;
      reread(w, { pushedAt: now(), upstream: Object.freeze({ state: "in_sync", ref, remote: "origin", ahead: 0, behind: 0 }) });
      if (echoesFirst) {
        announce(w);
        await macrotask();
      } else {
        setTimeout(() => announce(w), 0);
      }
      return w.state;
    },

    follow(projectId, onState, onStatus) {
      const follower = { projectId, onState, onStatus };
      followers.add(follower);
      queueMicrotask(() => followers.has(follower) && onStatus(FeedStatus.LIVE));
      return () => followers.delete(follower);
    },

    // Seeds are the pages' own wire JSON, in web/dev as from the server.
    decodeSeed: toSeed,
  };

  return {
    gateway,
    /** The code watcher read the worktree again: harness.git.change("ws1", { uncommitted: … }). Tells the feed. */
    change(workspaceId, fields = {}) {
      const w = workspace(workspaceId);
      reread(w, fields);
      return announce(w);
    },
    /** What the server would now read, without telling the feed (a change missed during a drop). */
    changeQuietly(workspaceId, fields = {}) {
      return reread(workspace(workspaceId), fields);
    },
    /** The remote moved: the counts a fetch will bring. */
    remoteMoves(workspaceId, counts) {
      workspace(workspaceId).remoteAfterFetch = counts;
    },
    /** Fetch and push fail with GIT_CREDENTIAL_MISSING for this workspace's origin, naming the host; null clears. */
    missCredential(workspaceId, details = { host: "github.com", kind: "https_token" }) {
      workspace(workspaceId).credentialMissing = details;
    },
    /** The next fetch or push fails with this code. */
    failNext(code, reason, details) {
      failure = { code, reason, details };
    },
    /** A fetch or a push takes this long. */
    slow(ms) {
      delay = ms;
    },
    /** Whether the feed echo of a fetch or a push lands before its answer. */
    echoFirst(on = true) {
      echoesFirst = on;
    },
    /** The workspace row is deleted. */
    forget(workspaceId) {
      spaces.delete(workspaceId);
    },
    addWorkspace,
    /** Tells every follower the feed's status, as a drop and a reconnect would. */
    reportStatus(status) {
      for (const f of [...followers]) f.onStatus(status);
    },
    find: (workspaceId) => spaces.get(workspaceId)?.state ?? null,
    /** The pushes that went through, oldest first. */
    pushed,
    calls,
  };
}
