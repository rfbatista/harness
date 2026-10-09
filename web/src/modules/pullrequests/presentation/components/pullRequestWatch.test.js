import { FeedStatus } from "../../../../shared/domain/feed.js";
import { mount, seededElement } from "../../../../shared/testing/alpine.js";
import { flush } from "../../../../shared/testing/doubles.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { memoryPullRequests } from "../../infrastructure/memory-gateway.js";
import { makePullRequest, makeWorkspace, P0, pullRequestDTO } from "../../testing/fixtures.js";
import { pullRequestBadge } from "./pullRequestBadge.js";
import { emptyStore, keep, pullRequestWatch } from "./pullRequestWatch.js";

file("pullrequests/presentation/pullRequestWatch");

async function setup({ seeded = [makePullRequest()], workspaceIds = ["ws1", "ws2"], world = {} } = {}) {
  const memory = memoryPullRequests({ workspaces: [makeWorkspace(), makeWorkspace({ workspaceId: "ws2" })], pullRequests: seeded, ...world });
  const store = emptyStore();
  const seed = { project_id: "p1", workspace_ids: workspaceIds, pull_requests: seeded.map(pullRequestDTO) };
  const { instance: watch } = mount(pullRequestWatch({ gateway: memory.gateway, store }), { el: seededElement(seed) });
  watch.init();
  await flush();
  const badge = (id) => pullRequestBadge({ store })(id);
  return { memory, store, watch, badge };
}

test("the seed fills each session's badge; a session without a PR has none", async () => {
  const { watch, badge } = await setup();
  const b = badge("ws1");
  assert.deepEqual([b.has, b.label, b.href, b.state, b.tone], [true, "PR #42", "https://github.com/acme/harness/pull/42", "open", "success"]);
  assert.equal(b.describe, "Pull request #42, open: Create pull request. Opens on GitHub.");
  assert.deepEqual([badge("ws2").has, badge("").has, badge("ws2").tone], [false, false, null]);
  watch.destroy();
});

test("a PR opened or changed on the feed shows on its session's badge at once", async () => {
  const { watch, badge, memory } = await setup();
  memory.open("ws2", { number: 50, draft: true });
  memory.setState("ws1", "merged");
  await flush();
  assert.deepEqual([badge("ws2").label, badge("ws2").state, badge("ws2").tone], ["PR #50", "draft", null]);
  assert.deepEqual([badge("ws1").state, badge("ws1").tone], ["merged", "signal"]);
  watch.destroy();
});

test("after a reconnect every workspace is read again, catching a PR opened during the drop", async () => {
  const { watch, badge, memory } = await setup({ seeded: [] });
  memory.openOnHost("ws2", { number: 7 }); // stored while the feed was down
  memory.reportStatus(FeedStatus.RESYNCED);
  await flush();
  assert.equal(badge("ws2").label, "PR #7");
  memory.forget("ws1");
  memory.reportStatus(FeedStatus.RESYNCED);
  await flush();
  assert.equal(badge("ws2").label, "PR #7", "a workspace gone meanwhile does not stop the others");
  watch.destroy();
});

test("an older PR's late news does not replace a newer one", () => {
  const store = emptyStore();
  const newer = makePullRequest({ number: 9, createdAt: new Date(P0.getTime() + 60_000) });
  keep(store, newer);
  keep(store, makePullRequest({ number: 7, state: "merged" }));
  assert.equal(store.byWorkspace.ws1.number, 9);
});

test("an unreadable seed leaves the badges empty, without failing", async () => {
  const store = emptyStore();
  const memory = memoryPullRequests();
  const { instance: watch } = mount(pullRequestWatch({ gateway: memory.gateway, store }), { el: seededElement({ pull_requests: [{ number: 1 }] }) });
  watch.init();
  assert.deepEqual(store.byWorkspace, {});
  watch.destroy();
});
