// The wire format of git state (Contract: worktree git state, fetch and push):
// GET /api/worktree_git_state, GET /api/list_worktree_git_states, the answers
// of POST /api/fetch_remote and /api/push_branch, the `git_state` feed key and
// the pages' seeds, and its mapping to the domain.

import { Codes, StructuredError } from "../../../shared/domain/errors.js";
import { UPSTREAM_STATES } from "../domain/gitState.js";

const count = (n) => (Number.isInteger(n) && n > 0 ? n : 0);
const nullableCount = (n) => (Number.isInteger(n) && n >= 0 ? n : null);
const text = (s) => (typeof s === "string" ? s : "");
const nullableText = (s) => (typeof s === "string" && s !== "" ? s : null);

function time(value, what) {
  const at = new Date(value);
  if (Number.isNaN(at.getTime())) bad(`${what} is not a time`);
  return at;
}

/** @returns {import("../domain/gitState.js").GitState} */
export function toGitState(dto) {
  if (!dto || typeof dto.workspace_id !== "string" || dto.workspace_id === "") bad("git state without a workspace_id");
  const at = `git state of ${dto.workspace_id}`;
  const up = dto.upstream;
  if (!up || !UPSTREAM_STATES.includes(up.state)) bad(`${at} has unknown upstream state "${up?.state}"`);
  const base = dto.base ?? {};
  const dirty = dto.uncommitted ?? {};
  return Object.freeze({
    workspaceId: dto.workspace_id,
    repositoryId: text(dto.repository_id),
    sessionId: text(dto.session_id),
    branch: text(dto.branch),
    head: text(dto.head),
    gone: dto.gone === true,
    base: Object.freeze({ ref: text(base.ref), ahead: count(base.ahead), behind: count(base.behind) }),
    upstream: Object.freeze({
      state: up.state,
      ref: nullableText(up.ref),
      remote: nullableText(up.remote),
      ahead: nullableCount(up.ahead),
      behind: nullableCount(up.behind),
    }),
    fetchedAt: dto.fetched_at ? time(dto.fetched_at, `${at}: fetched_at`) : null,
    uncommitted: Object.freeze({ tracked: count(dirty.tracked), untracked: count(dirty.untracked), staged: count(dirty.staged) }),
    readAt: time(dto.read_at, `${at}: read_at`),
  });
}

/** The wire shape of a state, for stubs and seeds in tests. */
export function toGitStateDTO(s) {
  return {
    workspace_id: s.workspaceId,
    repository_id: s.repositoryId,
    session_id: s.sessionId,
    branch: s.branch,
    head: s.head,
    gone: s.gone,
    base: { ref: s.base.ref, ahead: s.base.ahead, behind: s.base.behind },
    upstream: { state: s.upstream.state, ref: s.upstream.ref, remote: s.upstream.remote, ahead: s.upstream.ahead, behind: s.upstream.behind },
    fetched_at: s.fetchedAt ? s.fetchedAt.toISOString() : null,
    uncommitted: { tracked: s.uncommitted.tracked, untracked: s.uncommitted.untracked, staged: s.uncommitted.staged },
    read_at: s.readAt.toISOString(),
  };
}

/** GET /api/worktree_git_state and POST /api/push_branch → {"state": GitState} */
export function toStateAnswer(body) {
  if (!body?.state) bad("expected {state: {...}}");
  return toGitState(body.state);
}

/** GET /api/list_worktree_git_states → {"states": [GitState]} */
export function toStates(body) {
  if (!body || !Array.isArray(body.states)) bad("expected {states: [...]}");
  return body.states.map(toGitState);
}

/** POST /api/fetch_remote → {"fetched_at": RFC3339} */
export function toFetched(body) {
  if (!body?.fetched_at) bad("expected {fetched_at}");
  return { fetchedAt: time(body.fetched_at, "fetched_at") };
}

/**
 * A refusal as the module sees it: PUSH_REJECTED's and PUSH_STALE's
 * details.state (the state after a re-read) becomes a GitState. Others pass through.
 */
export function toPushError(err) {
  if (!(err instanceof StructuredError) || !err.details?.state) return err;
  if (err.code !== Codes.PUSH_REJECTED && err.code !== Codes.PUSH_STALE) return err;
  let state;
  try {
    state = toGitState(err.details.state);
  } catch {
    return new StructuredError(err.code, err.message, err.status, { ...err.details, state: undefined });
  }
  return new StructuredError(err.code, err.message, err.status, { ...err.details, state });
}

/** One feed message: the state it carries, or null for any other kind. Throws BAD_RESPONSE when it cannot be read. */
export function toGitStateEvent(dto) {
  if (!dto || typeof dto !== "object" || !dto.git_state) return null;
  return toGitState(dto.git_state);
}

/**
 * What the task page's `sessions-seed` carries for the chips: its project and
 * `git_states`, one GitState per session of the task that has a workspace (a
 * session page is a view of the same seed).
 * @returns {{ projectId: string, states: import("../domain/gitState.js").GitState[] }}
 */
export function toSeed(raw) {
  if (!raw || typeof raw !== "object") bad("expected a git state seed");
  const list = Array.isArray(raw.git_states) ? raw.git_states : [];
  return { projectId: text(raw.project_id), states: list.map(toGitState) };
}

function bad(detail) {
  throw new StructuredError(Codes.BAD_RESPONSE, `Unexpected git state: ${detail}.`);
}
