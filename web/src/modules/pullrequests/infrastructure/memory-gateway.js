// PullRequestGateway in memory: the fake for component tests and the data
// source for web/dev pages. It obeys the same contract as the real gateway
// (../testing/gateway-contract.js), and lets a test or the dev console play
// the rest of the world: another browser or the server's background check
// opening a PR, the host merging it, origin moving, a slow push.

import { Codes, StructuredError } from "../../../shared/domain/errors.js";
import { FeedStatus } from "../../../shared/domain/feed.js";
import { isOpen, newerOf } from "../domain/pullRequest.js";
import { toPanelSeed, toWatchSeed } from "./dto.js";

/** The contract's HTTP status per code, so the stub API answers as the server would. */
export const STATUS = Object.freeze({
  [Codes.INVALID_INPUT]: 400,
  [Codes.WORKSPACE_NOT_FOUND]: 404,
  [Codes.PR_NO_REMOTE]: 400,
  [Codes.PR_NO_COMMITS]: 409,
  [Codes.PR_BASE_NOT_FOUND]: 400,
  [Codes.PR_ALREADY_EXISTS]: 409,
  [Codes.GIT_CREDENTIAL_MISSING]: 400,
  [Codes.GIT_CREDENTIAL_REJECTED]: 502,
  [Codes.GIT_CREDENTIAL_STORE_LOCKED]: 503,
  [Codes.PR_PUSH_FAILED]: 502,
  [Codes.PR_CREATE_FAILED]: 502,
});

const REASONS = {
  [Codes.PR_NO_REMOTE]: "the repository has no GitHub origin remote",
  [Codes.PR_NO_COMMITS]: "the branch has no commits ahead of its base",
  [Codes.GIT_CREDENTIAL_MISSING]: "no stored credential resolves for the repository",
  [Codes.GIT_CREDENTIAL_REJECTED]: "Bad credentials",
  [Codes.PR_PUSH_FAILED]: "! [rejected] (non-fast-forward)",
  [Codes.PR_CREATE_FAILED]: "GitHub refused the pull request",
};

const refusal = (code, reason = REASONS[code] ?? "refused", details) => new StructuredError(code, `${code}: ${reason}`, STATUS[code] ?? 500, details);

/**
 * @typedef {object} MemoryWorkspace  a session's worktree and what the server knows of it
 * @property {string} id
 * @property {string} [projectId]
 * @property {string} [repositoryId]
 * @property {string} [headBranch]
 * @property {string} [baseBranch]
 * @property {string} [title]           the suggested title
 * @property {string} [body]            the suggested body
 * @property {number} [ahead]
 * @property {number} [uncommittedChanges]
 * @property {import("../domain/pullRequest.js").Remote | null} [remote]
 * @property {string[]} [blockers]      what the server would say locally
 * @property {string[]} [originBranches] the branches origin really has (default: the base)
 */

/**
 * @param {{
 *   workspaces?: MemoryWorkspace[],
 *   pullRequests?: import("../domain/pullRequest.js").PullRequest[],
 *   now?: () => Date,
 *   delayMs?: number,      how long a create takes
 *   echoFirst?: boolean,   whether the feed echo of a create lands before its answer
 * }} [world]
 */
export function memoryPullRequests({ workspaces = [], pullRequests = [], now = () => new Date(), delayMs = 0, echoFirst = false } = {}) {
  /** @type {Map<string, Required<MemoryWorkspace>>} */
  const spaces = new Map();
  /** Every PR by workspace, oldest first. */
  const prs = new Map();
  const followers = new Set();
  let failure = null;
  let delay = delayMs;
  let echoesFirst = echoFirst;
  let createCalls = 0;

  for (const w of workspaces) addWorkspace(w);
  for (const pr of pullRequests) prs.set(pr.workspaceId, [...(prs.get(pr.workspaceId) ?? []), pr]);

  function addWorkspace(w) {
    const head = w.headBranch ?? `agent/${w.id}`;
    const base = w.baseBranch ?? "main";
    spaces.set(w.id, {
      projectId: "p1",
      repositoryId: "r1",
      title: "",
      body: "",
      ahead: 1,
      uncommittedChanges: 0,
      remote: { host: "github.com", owner: "acme", repo: "harness" },
      blockers: [],
      originBranches: [base],
      ...w,
      headBranch: head,
      baseBranch: base,
    });
  }

  const newest = (id) => (prs.get(id) ?? []).reduce((a, b) => newerOf(a, b), null);

  function workspace(id) {
    const w = spaces.get(id);
    if (!w) throw new StructuredError(Codes.WORKSPACE_NOT_FOUND, `WORKSPACE_NOT_FOUND: workspace ${id} not found`, 404);
    return w;
  }

  function draftOf(w) {
    return Object.freeze({
      workspaceId: w.id,
      repositoryId: w.repositoryId,
      headBranch: w.headBranch,
      baseBranch: w.baseBranch,
      title: w.title,
      body: w.body,
      ahead: w.ahead,
      uncommittedChanges: w.uncommittedChanges,
      remote: w.remote,
      blockers: Object.freeze([...w.blockers]),
    });
  }

  /** Stores the PR and tells the project's followers, as the server's feed would. */
  function announce(pr) {
    const list = (prs.get(pr.workspaceId) ?? []).filter((x) => x.number !== pr.number || x.repositoryId !== pr.repositoryId);
    prs.set(pr.workspaceId, [...list, pr]);
    const projectId = spaces.get(pr.workspaceId)?.projectId ?? "";
    for (const f of [...followers]) if (f.projectId === projectId) f.onPullRequest(pr);
    return pr;
  }

  function nextNumber(repositoryId) {
    let n = 0;
    for (const list of prs.values()) for (const pr of list) if (pr.repositoryId === repositoryId) n = Math.max(n, pr.number);
    return n + 1;
  }

  function makePR(w, fields = {}) {
    const number = fields.number ?? nextNumber(w.repositoryId);
    const r = w.remote ?? { host: "github.com", owner: "acme", repo: "harness" };
    return Object.freeze({
      workspaceId: w.id,
      repositoryId: w.repositoryId,
      number,
      url: `https://${r.host}/${r.owner}/${r.repo}/pull/${number}`,
      title: w.title || w.headBranch,
      headBranch: w.headBranch,
      baseBranch: w.baseBranch,
      draft: false,
      state: "open",
      createdAt: now(),
      ...fields,
    });
  }

  const macrotask = () => new Promise((resolve) => setTimeout(resolve, 0));

  /** @type {import("../domain/ports.js").PullRequestGateway} */
  const gateway = {
    async get(workspaceId) {
      const w = workspace(workspaceId);
      return { draft: draftOf(w), pullRequest: newest(workspaceId) };
    },

    async create({ workspaceId, title, baseBranch = "", draft = false }) {
      createCalls++;
      if (!workspaceId) throw new StructuredError(Codes.INVALID_INPUT, "INVALID_INPUT: workspace_id is required", 400);
      if (title.trim() === "") throw new StructuredError(Codes.INVALID_INPUT, "INVALID_INPUT: a title is required", 400);
      const w = workspace(workspaceId);
      if (delay > 0) await new Promise((resolve) => setTimeout(resolve, delay));
      if (failure) {
        const f = failure;
        failure = null;
        throw refusal(f.code, f.reason, f.details);
      }
      const open = newest(workspaceId);
      if (isOpen(open)) throw refusal(Codes.PR_ALREADY_EXISTS, `#${open.number} is open for ${w.headBranch}`, { pullRequest: open });
      if (w.blockers.length > 0) throw refusal(w.blockers[0], undefined, w.blockers[0] === Codes.GIT_CREDENTIAL_MISSING ? { host: w.remote?.host ?? "", kind: "https_token" } : undefined);
      const base = baseBranch.trim() || w.baseBranch;
      if (!w.originBranches.includes(base)) throw refusal(Codes.PR_BASE_NOT_FOUND, `${base} is not a branch on origin`);
      if (w.ahead === 0) throw refusal(Codes.PR_NO_COMMITS, `${w.headBranch} has no commits ahead of ${base}`);

      const pr = makePR(w, { title: title.trim(), baseBranch: base, draft: draft === true });
      if (echoesFirst) {
        announce(pr);
        await macrotask();
      } else {
        prs.set(workspaceId, [...(prs.get(workspaceId) ?? []), pr]);
        setTimeout(() => announce(pr), 0);
      }
      return pr;
    },

    follow(projectId, onPullRequest, onStatus) {
      const follower = { projectId, onPullRequest, onStatus };
      followers.add(follower);
      queueMicrotask(() => followers.has(follower) && onStatus(FeedStatus.LIVE));
      return () => followers.delete(follower);
    },

    // Seeds are the pages' own wire JSON, in web/dev as from the server.
    decodePanelSeed: toPanelSeed,
    decodeWatchSeed: toWatchSeed,
  };

  return {
    gateway,
    /** A PR opened outside this page (another browser, gh, the server adopting one): harness.pr.open("ws1", { draft: true }). */
    open: (workspaceId, fields = {}) => announce(makePR(workspace(workspaceId), fields)),
    /** A PR is open on the host the feed has not told of yet: a create then finds it (PR_ALREADY_EXISTS). */
    openOnHost(workspaceId, fields = {}) {
      const pr = makePR(workspace(workspaceId), fields);
      prs.set(workspaceId, [...(prs.get(workspaceId) ?? []), pr]);
      return pr;
    },
    /** The host says the newest PR's state changed, as a background refresh finds it. */
    setState(workspaceId, state) {
      const pr = newest(workspaceId);
      return pr ? announce(Object.freeze({ ...pr, state })) : null;
    },
    /** What the server would now say locally: harness.pr.block("ws1", "GIT_CREDENTIAL_MISSING"); none clears. */
    block(workspaceId, ...codes) {
      workspace(workspaceId).blockers = codes;
    },
    /** Changes what git says of the workspace (ahead, uncommittedChanges, originBranches, …). */
    update(workspaceId, fields) {
      Object.assign(workspace(workspaceId), fields);
    },
    /** The next create fails with this code, whatever the draft said. */
    failNext(code, reason, details) {
      failure = { code, reason, details };
    },
    /** A create takes this long (push + host). */
    slow(ms) {
      delay = ms;
    },
    /** Whether the feed echo of a create lands before its answer. */
    echoFirst(on = true) {
      echoesFirst = on;
    },
    /** The workspace row is deleted; its PRs stay. */
    forget(workspaceId) {
      spaces.delete(workspaceId);
    },
    addWorkspace,
    /** Tells every follower the feed's status, as a drop and a reconnect would. */
    reportStatus(status) {
      for (const f of [...followers]) f.onStatus(status);
    },
    find: (workspaceId) => newest(workspaceId),
    get createCalls() {
      return createCalls;
    },
  };
}
