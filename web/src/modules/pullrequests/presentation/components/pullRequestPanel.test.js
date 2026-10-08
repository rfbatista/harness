import { Codes } from "../../../../shared/domain/errors.js";
import { FeedStatus } from "../../../../shared/domain/feed.js";
import { mount, seededElement } from "../../../../shared/testing/alpine.js";
import { flush } from "../../../../shared/testing/doubles.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { memoryPullRequests } from "../../infrastructure/memory-gateway.js";
import { draftDTO, makePullRequest, makeWorkspace, pullRequestDTO } from "../../testing/fixtures.js";
import { pullRequestPanel } from "./pullRequestPanel.js";

file("pullrequests/presentation/pullRequestPanel");

/** The history page as the server paints it: the seed carries the view unless painted = false. */
async function setup({ workspace = {}, pullRequests = [], painted = true, ...world } = {}) {
  const memory = memoryPullRequests({ workspaces: [makeWorkspace(workspace)], pullRequests, ...world });
  const seed = { project_id: "p1", workspace_id: "ws1" };
  if (painted) {
    const { draft, pullRequest } = await memory.gateway.get("ws1");
    Object.assign(seed, { draft: draftDTO(draft), pull_request: pullRequest ? pullRequestDTO(pullRequest) : null });
  }
  const mounted = mount(pullRequestPanel({ gateway: memory.gateway }), { el: seededElement(seed) });
  mounted.instance.init();
  await flush();
  return { ...mounted, memory, panel: mounted.instance };
}

test("a branch with commits and no PR offers the create; the form starts from the draft", async () => {
  const { panel } = await setup({ workspace: { uncommittedChanges: 2 } });
  assert.deepEqual([panel.hidden, panel.offersCreate, panel.cannotCreate, panel.hasChip, panel.blocked], [false, true, false, false, false]);
  assert.equal(panel.showsCreateButton, true);
  panel.openForm();
  assert.deepEqual([panel.showsForm, panel.form.title, panel.form.baseBranch, panel.form.draft], [true, "Create pull request", "main", false]);
  assert.equal(panel.showsCreateButton, false, "the form has its own button");
  assert.equal(panel.summary, "3 commits from agent/create-pull-request-9d728ce8 into main on acme/harness");
  assert.equal(panel.uncommitted, "2 uncommitted changes in the worktree will not be in the pull request.");
  panel.destroy();
});

test("creating shows progress, sends one POST for a double submit, then shows the PR", async () => {
  const { panel, memory } = await setup({ delayMs: 20 });
  panel.openForm();
  panel.form.title = "  Add the panel ";
  panel.form.draft = true;
  const first = panel.submit();
  const second = panel.submit();
  assert.deepEqual([panel.creating, panel.buttonLabel, panel.busyAttr, panel.disabledAttr], [true, "Creating…", "true", "true"]);
  assert.ok(panel.progress.startsWith("Pushing agent/create-pull-request-9d728ce8"));
  await Promise.all([first, second]);
  assert.equal(memory.createCalls, 1);
  assert.deepEqual([panel.showsForm, panel.chip.label, panel.chip.state, panel.chip.title, panel.offersCreate], [false, "#1", "draft", "Add the panel", false]);
  assert.equal(panel.announcement, "Opened pull request #1.");
  panel.destroy();
});

test("a draft whose blockers say no disables the create and says why, one reason each", async () => {
  const { panel } = await setup({ workspace: { ahead: 0, remote: null, blockers: [Codes.PR_NO_COMMITS, Codes.PR_NO_REMOTE, Codes.PR_TOOL_UNAVAILABLE, "PR_FROM_THE_FUTURE"] } });
  assert.deepEqual([panel.offersCreate, panel.cannotCreate, panel.disabledAttr], [true, true, "true"]);
  assert.deepEqual(
    panel.blockers.map((b) => b.text),
    [
      "Nothing to propose yet: agent/create-pull-request-9d728ce8 has no commits ahead of main.",
      "This repository has no GitHub origin remote, so there is nowhere to open a pull request.",
      "The server can't reach GitHub as you: gh is missing or not signed in on the server machine. Run gh auth login there.",
      "The server would refuse it (PR_FROM_THE_FUTURE).",
    ],
  );
  panel.openForm();
  assert.equal(panel.showsForm, false, "the form does not open");
  panel.destroy();
});

test("an open PR shows as a chip linked to the host, with no create", async () => {
  const { panel } = await setup({ pullRequests: [makePullRequest()] });
  assert.deepEqual([panel.chip.label, panel.chip.href, panel.chip.tone, panel.offersCreate, panel.blocked], ["#42", "https://github.com/acme/harness/pull/42", "success", false, false]);
  panel.destroy();
});

test("a merged PR shows beside the create, which may open another", async () => {
  const { panel, memory } = await setup({ pullRequests: [makePullRequest({ state: "merged" })] });
  assert.deepEqual([panel.chip.state, panel.chip.tone, panel.offersCreate, panel.cannotCreate], ["merged", "signal", true, false]);
  panel.openForm();
  await panel.submit();
  assert.equal(memory.createCalls, 1);
  assert.deepEqual([panel.chip.number, panel.chip.state], [43, "open"]);
  panel.destroy();
});

test("painted without a view (the server had none), the panel reads it", async () => {
  const { panel } = await setup({ painted: false });
  assert.deepEqual([panel.hidden, panel.offersCreate], [false, true]);
  panel.destroy();
});

test("a workspace that is gone shows no PR action and no error", async () => {
  const memory = memoryPullRequests({ workspaces: [] });
  const { instance: panel } = mount(pullRequestPanel({ gateway: memory.gateway }), { el: seededElement({ project_id: "p1", workspace_id: "ws1" }) });
  panel.init();
  await flush();
  assert.deepEqual([panel.gone, panel.hidden, panel.error], [true, true, null]);
  panel.destroy();
});

test("the form needs a title: the field says so, nothing is sent, the input stays", async () => {
  const { panel, memory } = await setup();
  panel.openForm();
  panel.form.title = "  ";
  panel.form.body = "Keep me";
  await panel.submit();
  assert.deepEqual([panel.problems.title, panel.titleInvalid, memory.createCalls, panel.form.body, panel.showsForm], ["A pull request needs a title.", "true", 0, "Keep me", true]);
  panel.destroy();
});

test("origin moved since the draft: a missing base lands on the base field; no commits in a banner; input kept", async () => {
  const { panel, memory } = await setup();
  panel.openForm();
  panel.form.title = "Mine";
  panel.form.baseBranch = "develop";
  await panel.submit();
  assert.deepEqual([panel.problems.baseBranch, panel.baseInvalid, panel.refusal, panel.form.title], ["develop is not a branch on origin.", "true", null, "Mine"]);

  panel.form.baseBranch = "";
  memory.failNext(Codes.PR_NO_COMMITS, "agent/x has no commits ahead of main");
  await panel.submit();
  assert.deepEqual([panel.problems.baseBranch, panel.refusal.code, panel.refusal.lead, panel.showsForm], ["", Codes.PR_NO_COMMITS, "Nothing to propose: origin already has every commit of this branch.", true]);
  panel.destroy();
});

test("the push's, the host's and the tool's refusals show their reason and the code; a retry may follow", async () => {
  const { panel, memory } = await setup();
  panel.openForm();
  memory.failNext(Codes.PR_TOOL_UNAVAILABLE, "gh is not signed in; run `gh auth login` on the server machine");
  await panel.submit();
  assert.deepEqual(panel.refusal, { lead: "The server can't reach GitHub as you.", reason: "gh is not signed in; run `gh auth login` on the server machine", code: Codes.PR_TOOL_UNAVAILABLE });
  memory.failNext(Codes.PR_PUSH_FAILED, "! [rejected] (non-fast-forward)");
  await panel.submit();
  assert.deepEqual([panel.refusal.lead, panel.refusal.reason], ["Git refused the push.", "! [rejected] (non-fast-forward)"]);
  memory.failNext(Codes.PR_CREATE_FAILED, "Validation Failed");
  await panel.submit();
  assert.equal(panel.refusal.lead, "GitHub refused the pull request.");
  await panel.submit();
  assert.deepEqual([panel.refusal, panel.chip.label], [null, "#1"], "the retry went through");
  panel.destroy();
});

test("a PR already open on the host is adopted, not an error", async () => {
  const { panel, memory } = await setup();
  panel.openForm();
  memory.openOnHost("ws1", { number: 9 }); // outside the harness; the feed has not told yet
  await panel.submit();
  assert.deepEqual([panel.refusal, panel.chip.number, panel.showsForm], [null, 9, false]);
  assert.equal(panel.notice, "#9 was already open on GitHub; it is linked here now.");
  panel.destroy();
});

for (const echoFirst of [true, false]) {
  test(`the person's own create ${echoFirst ? "echoed before its answer" : "answered before its echo"}: one PR, no "elsewhere"`, async () => {
    const { panel } = await setup({ echoFirst });
    panel.openForm();
    await panel.submit();
    await flush();
    assert.deepEqual([panel.chip.label, panel.notice, panel.showsForm], ["#1", "", false]);
    panel.destroy();
  });
}

test("the echo says a PR is open but the POST failed: the PR wins", async () => {
  const { panel, memory } = await setup({ delayMs: 10 });
  panel.openForm();
  const sent = panel.submit();
  memory.open("ws1", { number: 4 }); // lands while the create is in flight
  memory.failNext(Codes.PR_CREATE_FAILED, "timeout");
  await sent;
  assert.deepEqual([panel.refusal, panel.chip.number, panel.notice, panel.showsForm], [null, 4, "", false]);
  panel.destroy();
});

test("a PR opened elsewhere while the form is open closes it and says so; with the form closed it just shows", async () => {
  const { panel, memory } = await setup();
  panel.openForm();
  memory.open("ws1", { number: 5 });
  await flush();
  assert.deepEqual([panel.showsForm, panel.notice, panel.chip.number], [false, "Pull request #5 was opened from elsewhere.", 5]);
  memory.setState("ws1", "merged");
  await flush();
  assert.deepEqual([panel.chip.state, panel.notice, panel.offersCreate, panel.announcement], ["merged", "Pull request #5 was opened from elsewhere.", true, "Pull request #5 is now merged."]);
  memory.open("ws1", { number: 6 });
  await flush();
  assert.deepEqual([panel.chip.number, panel.showsForm, panel.announcement], [6, false, "Pull request #6 is open."]);
  panel.destroy();
});

test("another workspace's PR is not this page's", async () => {
  const memory = memoryPullRequests({ workspaces: [makeWorkspace(), makeWorkspace({ workspaceId: "ws2" })] });
  const { draft } = await memory.gateway.get("ws1");
  const { instance: panel } = mount(pullRequestPanel({ gateway: memory.gateway }), { el: seededElement({ project_id: "p1", workspace_id: "ws1", draft: draftDTO(draft), pull_request: null }) });
  panel.init();
  await flush();
  memory.open("ws2");
  await flush();
  assert.equal(panel.pullRequest, null);
  panel.destroy();
});

test("opening the form reads the draft again without touching what the person typed", async () => {
  const { panel, memory } = await setup();
  panel.openForm();
  panel.form.title = "Typed";
  panel.cancel();
  memory.update("ws1", { ahead: 5, uncommittedChanges: 1, title: "Server suggestion" });
  panel.openForm();
  await flush();
  assert.deepEqual([panel.form.title, panel.draft.ahead, panel.uncommitted], ["Typed", 5, "1 uncommitted change in the worktree will not be in the pull request."]);
  panel.destroy();
});

test("after a reconnect the panel reads the workspace again; nothing polls meanwhile", async () => {
  const { panel, memory } = await setup();
  memory.update("ws1", { blockers: [Codes.PR_TOOL_UNAVAILABLE] });
  await flush();
  assert.equal(panel.blocked, false, "no polling: still what was read");
  memory.reportStatus(FeedStatus.RESYNCED);
  await flush();
  assert.equal(panel.blocked, true);
  panel.destroy();
});

test("an unreadable seed shows the error instead of the control", async () => {
  const memory = memoryPullRequests({ workspaces: [makeWorkspace()] });
  const { instance: panel } = mount(pullRequestPanel({ gateway: memory.gateway }), { el: seededElement({ project_id: "p1" }) });
  panel.init();
  assert.deepEqual([panel.error.code, panel.hidden], [Codes.BAD_RESPONSE, true]);
});
