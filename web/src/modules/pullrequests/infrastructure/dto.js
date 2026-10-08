// The wire format of pull requests (the Web UI contract: GET /api/pull_request,
// POST /api/create_pull_request, the project feed's `pull_request` messages and
// the pages' seeds) and its mapping to the domain.

import { Codes, StructuredError } from "../../../shared/domain/errors.js";
import { STATES } from "../domain/pullRequest.js";

/** @returns {import("../domain/pullRequest.js").PullRequest} */
export function toPullRequest(dto) {
  if (!dto || typeof dto.workspace_id !== "string" || dto.workspace_id === "") bad("pull request without a workspace_id");
  const at = `pull request ${dto.workspace_id}#${dto.number}`;
  if (!Number.isInteger(dto.number) || dto.number <= 0) bad(`${at} has no number`);
  if (!STATES.includes(dto.state)) bad(`${at} has unknown state "${dto.state}"`);
  const createdAt = new Date(dto.created_at);
  if (Number.isNaN(createdAt.getTime())) bad(`${at}: created_at is not a time`);
  return Object.freeze({
    workspaceId: dto.workspace_id,
    repositoryId: dto.repository_id ?? "",
    number: dto.number,
    url: typeof dto.url === "string" ? dto.url : "",
    title: dto.title ?? "",
    headBranch: dto.head_branch ?? "",
    baseBranch: dto.base_branch ?? "",
    draft: dto.draft === true,
    state: dto.state,
    createdAt,
  });
}

/** @returns {import("../domain/pullRequest.js").PullRequestDraft} */
export function toDraft(dto) {
  if (!dto || typeof dto.workspace_id !== "string" || dto.workspace_id === "") bad("draft without a workspace_id");
  const r = dto.remote;
  return Object.freeze({
    workspaceId: dto.workspace_id,
    repositoryId: dto.repository_id ?? "",
    headBranch: dto.head_branch ?? "",
    baseBranch: dto.base_branch ?? "",
    title: dto.title ?? "",
    body: dto.body ?? "",
    ahead: count(dto.ahead),
    uncommittedChanges: count(dto.uncommitted_changes),
    remote: r && typeof r === "object" ? Object.freeze({ host: r.host ?? "", owner: r.owner ?? "", repo: r.repo ?? "" }) : null,
    blockers: Object.freeze(Array.isArray(dto.blockers) ? dto.blockers.filter((c) => typeof c === "string" && c !== "") : []),
  });
}

/** GET /api/pull_request → {"draft": {...}, "pull_request": {...} | null} */
export function toView(body) {
  if (!body?.draft) bad("expected {draft: {...}, pull_request}");
  return { draft: toDraft(body.draft), pullRequest: body.pull_request ? toPullRequest(body.pull_request) : null };
}

/** POST /api/create_pull_request → {"pull_request": {...}} */
export function toCreated(body) {
  if (!body?.pull_request) bad("expected {pull_request: {...}}");
  return toPullRequest(body.pull_request);
}

/** The create request's body; an empty base is left out, so the server takes the draft's. */
export function createBody({ workspaceId, title, body, baseBranch, draft }) {
  return { workspace_id: workspaceId, title, body, base_branch: baseBranch.trim() || undefined, draft };
}

/**
 * A refusal as the module sees it: PR_ALREADY_EXISTS's details.pull_request
 * becomes details.pullRequest. Other errors pass through.
 */
export function toCreateError(err) {
  if (!(err instanceof StructuredError) || err.code !== Codes.PR_ALREADY_EXISTS || !err.details?.pull_request) return err;
  let pullRequest;
  try {
    pullRequest = toPullRequest(err.details.pull_request);
  } catch {
    return err;
  }
  return new StructuredError(err.code, err.message, err.status, { pullRequest });
}

/** One feed message: the PR it carries, or null for any other kind. Throws BAD_RESPONSE when it cannot be read. */
export function toPullRequestEvent(dto) {
  if (!dto || typeof dto !== "object" || !dto.pull_request) return null;
  return toPullRequest(dto.pull_request);
}

/** The history page's seed: {project_id, workspace_id, draft?, pull_request?}. */
export function toPanelSeed(raw) {
  if (!raw || typeof raw.workspace_id !== "string") bad("expected a pull request seed with a workspace_id");
  return {
    projectId: raw.project_id ?? "",
    workspaceId: raw.workspace_id,
    view: raw.draft ? toView(raw) : null,
  };
}

/** The task page's seed: {project_id, workspace_ids, pull_requests}. */
export function toWatchSeed(raw) {
  if (!raw || typeof raw !== "object") bad("expected a pull requests seed");
  return {
    projectId: raw.project_id ?? "",
    workspaceIds: Array.isArray(raw.workspace_ids) ? raw.workspace_ids.filter((id) => typeof id === "string" && id !== "") : [],
    pullRequests: Array.isArray(raw.pull_requests) ? raw.pull_requests.map(toPullRequest) : [],
  };
}

const count = (n) => (Number.isInteger(n) && n > 0 ? n : 0);

function bad(detail) {
  throw new StructuredError(Codes.BAD_RESPONSE, `Unexpected pull request: ${detail}.`);
}
