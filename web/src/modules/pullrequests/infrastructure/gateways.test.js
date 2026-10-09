// Both PullRequestGateway implementations run the same contract.

import { Codes, StructuredError } from "../../../shared/domain/errors.js";
import { apiClient } from "../../../shared/infrastructure/api.js";
import { feed } from "../../../shared/infrastructure/feed.js";
import { flush } from "../../../shared/testing/doubles.js";
import { assert, file, test } from "../../../shared/testing/test.js";
import { pullRequestGatewayContract } from "../testing/gateway-contract.js";
import { draftDTO, makeDraft, makePullRequest, makeWorkspace, pullRequestDTO } from "../testing/fixtures.js";
import { stubPullRequestsApi } from "../testing/stub-api.js";
import { createBody, toCreateError, toDraft, toPanelSeed, toPullRequest, toPullRequestEvent, toView, toWatchSeed } from "./dto.js";
import { memoryPullRequests } from "./memory-gateway.js";
import { pullRequestsGateway } from "./pullrequests-gateway.js";

file("pullrequests/infrastructure/gateways");

pullRequestGatewayContract("memory", (world) => {
  const memory = memoryPullRequests(world);
  return { gateway: memory.gateway, world: memory };
});

function http(world) {
  const stub = stubPullRequestsApi(world);
  const gateway = pullRequestsGateway(apiClient({ base: "/api", fetch: stub.fetch }), feed({ base: "/api", EventSource: stub.EventSource }));
  return { gateway, stub };
}

pullRequestGatewayContract("http", (world) => {
  const { gateway, stub } = http(world);
  return { gateway, world: stub.memory };
});

test("http · create posts the contract's body; an empty base is left out", async () => {
  const { gateway, stub } = http({ workspaces: [makeWorkspace()] });
  await gateway.create({ workspaceId: "ws1", title: "T", body: "B", baseBranch: " ", draft: true });
  assert.deepEqual(stub.posted, [{ workspace_id: "ws1", title: "T", body: "B", draft: true }]);
});

test("http · a malformed pull request on the feed is dropped, the stream stays open", async () => {
  const { gateway, stub } = http({ workspaces: [makeWorkspace()] });
  const seen = [];
  const close = gateway.follow("p1", (pr) => seen.push(pr.number), () => {});
  await flush();
  stub.pushRaw({ pull_request: { workspace_id: "ws1", number: 0, state: "open" } });
  stub.pushRaw({ pull_request: { workspace_id: "ws1", number: 3, state: "reopened", created_at: "2026-10-07T21:00:00Z" } });
  stub.pushRaw({ review_request: { id: "rv1" } });
  stub.memory.open("ws1", { number: 8 });
  await flush();
  assert.deepEqual(seen, [8]);
  close();
});

test("a pull request reads from the wire; optional fields read as empty", () => {
  const pr = toPullRequest({ workspace_id: "ws1", number: 3, state: "merged", created_at: "2026-10-07T21:00:00Z" });
  assert.deepEqual([pr.url, pr.title, pr.draft, pr.state, pr.createdAt.toISOString()], ["", "", false, "merged", "2026-10-07T21:00:00.000Z"]);
  assert.ok(Object.isFrozen(pr));
  const back = toPullRequest(pullRequestDTO(makePullRequest({ draft: true })));
  assert.deepEqual(back, makePullRequest({ draft: true }));
});

test("a malformed pull request, draft or answer is BAD_RESPONSE", () => {
  const dto = pullRequestDTO(makePullRequest());
  assert.throws(() => toPullRequest({ ...dto, workspace_id: "" }), Codes.BAD_RESPONSE);
  assert.throws(() => toPullRequest({ ...dto, number: "42" }), Codes.BAD_RESPONSE);
  assert.throws(() => toPullRequest({ ...dto, state: "reopened" }), Codes.BAD_RESPONSE);
  assert.throws(() => toPullRequest({ ...dto, created_at: "yesterday" }), Codes.BAD_RESPONSE);
  assert.throws(() => toDraft({}), Codes.BAD_RESPONSE);
  assert.throws(() => toView({ pull_request: null }), Codes.BAD_RESPONSE);
});

test("a draft reads from the wire: no origin is a null remote, odd counts are 0, blockers are codes", () => {
  const d = toDraft({ ...draftDTO(makeDraft()), remote: null, ahead: -1, uncommitted_changes: "2", blockers: ["PR_NO_REMOTE", "", 7] });
  assert.deepEqual([d.remote, d.ahead, d.uncommittedChanges, d.blockers], [null, 0, 0, ["PR_NO_REMOTE"]]);
  assert.deepEqual(toDraft(draftDTO(makeDraft())), makeDraft());
});

test("a feed message is a pull request only under its key", () => {
  const dto = pullRequestDTO(makePullRequest());
  assert.equal(toPullRequestEvent({ pull_request: dto }).number, 42);
  assert.equal(toPullRequestEvent({ ticket: { id: "t1" } }), null);
  assert.equal(toPullRequestEvent({ pull_request: null }), null);
});

test("PR_ALREADY_EXISTS carries the open PR in the domain's shape; other errors pass through", () => {
  const raw = new StructuredError(Codes.PR_ALREADY_EXISTS, "PR_ALREADY_EXISTS: #42", 409, { pull_request: pullRequestDTO(makePullRequest()) });
  const err = toCreateError(raw);
  assert.deepEqual([err.code, err.status, err.details.pullRequest.number], [Codes.PR_ALREADY_EXISTS, 409, 42]);
  const other = new StructuredError(Codes.PR_PUSH_FAILED, "PR_PUSH_FAILED: no", 502);
  assert.equal(toCreateError(other), other);
  const unreadable = new StructuredError(Codes.PR_ALREADY_EXISTS, "x", 409, { pull_request: { number: 1 } });
  assert.equal(toCreateError(unreadable), unreadable, "kept as it came when its PR cannot be read");
});

test("the create body trims the base and leaves it out when empty", () => {
  assert.deepEqual(createBody({ workspaceId: "ws1", title: "T", body: "", baseBranch: " develop ", draft: false }), { workspace_id: "ws1", title: "T", body: "", base_branch: "develop", draft: false });
});

test("the pages' seeds: the history page's with or without a view, the task page's never null", () => {
  const view = { draft: draftDTO(makeDraft()), pull_request: pullRequestDTO(makePullRequest()) };
  const panel = toPanelSeed({ project_id: "p1", workspace_id: "ws1", ...view });
  assert.deepEqual([panel.projectId, panel.workspaceId, panel.view.pullRequest.number, panel.view.draft.ahead], ["p1", "ws1", 42, 3]);
  assert.equal(toPanelSeed({ project_id: "p1", workspace_id: "ws1" }).view, null, "painted without one: read it");
  assert.throws(() => toPanelSeed({ project_id: "p1" }), Codes.BAD_RESPONSE);

  const watch = toWatchSeed({ project_id: "p1", workspace_ids: ["ws1", "", "ws2"], pull_requests: [pullRequestDTO(makePullRequest())] });
  assert.deepEqual([watch.workspaceIds, watch.pullRequests.map((p) => p.number)], [["ws1", "ws2"], [42]]);
  assert.deepEqual(toWatchSeed({ project_id: "p1", workspace_ids: null, pull_requests: null }), { projectId: "p1", workspaceIds: [], pullRequests: [] });
});
