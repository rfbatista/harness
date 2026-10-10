// The PullRequestGateway contract (../domain/ports.js), run by every
// implementation. A fake that passes it behaves like the real gateway as far
// as the history page's panel and the task page's badges can tell.

import { Codes } from "../../../shared/domain/errors.js";
import { FeedStatus } from "../../../shared/domain/feed.js";
import { flush } from "../../../shared/testing/doubles.js";
import { assert, test } from "../../../shared/testing/test.js";
import { makePullRequest, makeWorkspace } from "./fixtures.js";

/**
 * @typedef {object} Subject
 * @property {import("../domain/ports.js").PullRequestGateway} gateway
 * @property {ReturnType<typeof import("../infrastructure/memory-gateway.js").memoryPullRequests>} world  plays the server's side
 */

/** @param {string} name @param {(world: object) => Subject} makeSubject */
export function pullRequestGatewayContract(name, makeSubject) {
  const contract = (title, fn) => test(`${name} · ${title}`, fn);
  const create = (gateway, fields = {}) => gateway.create({ workspaceId: "ws1", title: "Add the panel", body: "", baseBranch: "", draft: false, ...fields });

  contract("get: the draft as git sees it, and no PR yet", async () => {
    const { gateway } = makeSubject({ workspaces: [makeWorkspace({ ahead: 2, uncommittedChanges: 1 })] });
    const { draft, pullRequest } = await gateway.get("ws1");
    assert.equal(pullRequest, null);
    assert.deepEqual(
      [draft.workspaceId, draft.headBranch, draft.baseBranch, draft.ahead, draft.uncommittedChanges, draft.remote, draft.blockers],
      ["ws1", "agent/create-pull-request-9d728ce8", "main", 2, 1, { host: "github.com", owner: "acme", repo: "harness" }, []],
    );
  });

  contract("get: the newest PR stored for the workspace, a merged one before a later open one", async () => {
    const { gateway } = makeSubject({
      workspaces: [makeWorkspace()],
      pullRequests: [makePullRequest({ number: 7, state: "merged" }), makePullRequest({ number: 9, createdAt: new Date("2026-10-07T22:00:00Z") })],
    });
    const { pullRequest } = await gateway.get("ws1");
    assert.deepEqual([pullRequest.number, pullRequest.state, pullRequest.url], [9, "open", "https://github.com/acme/harness/pull/42"]);
    assert.ok(pullRequest.createdAt instanceof Date);
  });

  contract("get: a workspace that is gone is WORKSPACE_NOT_FOUND", async () => {
    const { gateway } = makeSubject({ workspaces: [] });
    await assert.rejects(gateway.get("ws-gone"), Codes.WORKSPACE_NOT_FOUND);
  });

  contract("create opens the PR with the title, base and draft asked for; get then returns it", async () => {
    const { gateway } = makeSubject({ workspaces: [makeWorkspace({ originBranches: ["main", "develop"] })] });
    const pr = await create(gateway, { title: "  Add the panel  ", baseBranch: "develop", draft: true });
    assert.deepEqual([pr.workspaceId, pr.title, pr.baseBranch, pr.draft, pr.state], ["ws1", "Add the panel", "develop", true, "open"]);
    assert.ok(pr.number > 0 && pr.url.startsWith("https://"));
    assert.equal((await gateway.get("ws1")).pullRequest.number, pr.number);
  });

  contract("create with an empty base takes the draft's", async () => {
    const { gateway } = makeSubject({ workspaces: [makeWorkspace()] });
    assert.equal((await create(gateway, { baseBranch: "  " })).baseBranch, "main");
  });

  contract("create needs a title, and a workspace that exists", async () => {
    const { gateway } = makeSubject({ workspaces: [makeWorkspace()] });
    await assert.rejects(create(gateway, { title: "   " }), Codes.INVALID_INPUT);
    await assert.rejects(create(gateway, { workspaceId: "ws-gone" }), Codes.WORKSPACE_NOT_FOUND);
  });

  contract("create is refused with the draft's blocker, and when origin has moved since the draft", async () => {
    const { gateway, world } = makeSubject({ workspaces: [makeWorkspace({ blockers: ["GIT_CREDENTIAL_MISSING"] })] });
    const err = await assert.rejects(create(gateway), Codes.GIT_CREDENTIAL_MISSING);
    assert.ok(err.message.startsWith("GIT_CREDENTIAL_MISSING"), "the message starts with the code");
    assert.deepEqual(err.details, { host: "github.com", kind: "https_token" }, "it says what to add");
    world.block("ws1");
    await assert.rejects(create(gateway, { baseBranch: "gone" }), Codes.PR_BASE_NOT_FOUND);
    world.update("ws1", { ahead: 0 });
    await assert.rejects(create(gateway), Codes.PR_NO_COMMITS);
  });

  contract("create surfaces the push's and the host's refusals with their reasons", async () => {
    const { gateway, world } = makeSubject({ workspaces: [makeWorkspace()] });
    world.failNext(Codes.PR_PUSH_FAILED, "! [rejected] main -> main (non-fast-forward)");
    const err = await assert.rejects(create(gateway), Codes.PR_PUSH_FAILED);
    assert.ok(err.message.includes("non-fast-forward"));
    world.failNext(Codes.PR_CREATE_FAILED, "Validation Failed");
    await assert.rejects(create(gateway), Codes.PR_CREATE_FAILED);
  });

  contract("an open PR already there is PR_ALREADY_EXISTS, carrying it", async () => {
    const { gateway } = makeSubject({ workspaces: [makeWorkspace()], pullRequests: [makePullRequest({ number: 5 })] });
    const err = await assert.rejects(create(gateway), Codes.PR_ALREADY_EXISTS);
    assert.deepEqual([err.details.pullRequest.number, err.details.pullRequest.state], [5, "open"]);
  });

  contract("a merged PR does not stop another", async () => {
    const { gateway } = makeSubject({ workspaces: [makeWorkspace()], pullRequests: [makePullRequest({ number: 5, state: "merged" })] });
    const pr = await create(gateway);
    assert.equal(pr.state, "open");
    assert.ok(pr.number !== 5);
  });

  contract("follow reports live, delivers the project's PRs opened and changed, and stops when closed", async () => {
    const { gateway, world } = makeSubject({ workspaces: [makeWorkspace(), makeWorkspace({ workspaceId: "ws-other", projectId: "p2" })] });
    const seen = [];
    const statuses = [];
    const close = gateway.follow("p1", (pr) => seen.push(`${pr.workspaceId}#${pr.number}:${pr.state}`), (s) => statuses.push(s));
    await flush();
    assert.ok(statuses.includes(FeedStatus.LIVE), "reports live once connected");

    const pr = await create(gateway);
    await flush();
    world.open("ws-other");
    world.setState("ws1", "merged");
    await flush();
    assert.deepEqual(seen, [`ws1#${pr.number}:open`, `ws1#${pr.number}:merged`]);

    close();
    world.setState("ws1", "closed");
    await flush();
    assert.equal(seen.length, 2, "nothing after close");
  });
}
