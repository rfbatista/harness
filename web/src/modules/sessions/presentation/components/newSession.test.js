import { Codes } from "../../../../shared/domain/errors.js";
import { mount } from "../../../../shared/testing/alpine.js";
import { flush } from "../../../../shared/testing/doubles.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { memoryGateway } from "../../infrastructure/memory-gateway.js";
import { newSession } from "./newSession.js";

file("sessions/presentation/newSession");

const BRANCHES = {
  r1: [
    { name: "main", remote: false, isHead: true },
    { name: "feat/feed", remote: false, isHead: false },
    { name: "origin/main", remote: true, isHead: false },
  ],
  r2: [{ name: "develop", remote: false, isHead: true }],
};

function setup({ defaultRepository = "r1", repositories = { r1: "p1", r2: "p1" }, branches = BRANCHES } = {}) {
  const memory = memoryGateway({ projects: ["p1"], repositories, branches });
  const form = document.createElement("form");
  if (defaultRepository !== null) form.dataset.defaultRepository = defaultRepository;
  const mounted = mount(() => newSession({ gateway: memory.gateway })("p1", "t1"), { el: form });
  mounted.instance.init();
  return { ...mounted, memory };
}

test("starts an interactive session on the page's task and reports it", async () => {
  const { instance, dispatched, memory } = setup();
  assert.equal(instance.repositoryId, "r1", "the default repository is preselected");
  assert.equal(instance.cannotSubmit, false, "the first message is optional");
  assert.equal(instance.autoAccept, "off", "approvals are answered in the terminal");
  instance.prompt = "  write the plan  ";
  instance.agentId = "a-reviewer";
  await instance.submit();

  const [event] = dispatched;
  assert.equal(event.name, "session-created");
  assert.deepEqual(
    [event.detail.session.ticketId, event.detail.session.task, event.detail.session.agentId, event.detail.session.runsOn],
    ["t1", "write the plan", "a-reviewer", "server"],
  );
  assert.equal((await memory.gateway.list({ projectId: "p1", ticketId: "t1" })).length, 1);
});

test("architect mode starts from the task without a first message", async () => {
  const { instance, dispatched } = setup();
  assert.equal(instance.mode, "", "the default mode runs the agent as it is");
  instance.mode = "architect";
  assert.ok(instance.promptPlaceholder.includes("architect starts from the task"));
  assert.ok(instance.modeHint.includes("task-architecture"));
  await instance.submit();
  assert.equal(dispatched[0].detail.session.mode, "architect");
});

test("design mode tells the agent to publish as it goes", async () => {
  const { instance, dispatched } = setup();
  instance.mode = "design";
  assert.ok(instance.design);
  assert.ok(instance.promptPlaceholder.includes("Design tab"), instance.promptPlaceholder);
  assert.ok(instance.modeHint.includes("publishes"), instance.modeHint);
  await instance.submit();
  assert.equal(dispatched[0].detail.session.mode, "design");
});

test("a refused start shows the coded error and keeps the draft", async () => {
  const { instance, dispatched } = setup({ repositories: { r1: "another-project" } });
  instance.prompt = "go";
  await instance.submit();
  assert.equal(instance.error.code, Codes.CROSS_PROJECT_ACCESS);
  assert.equal(instance.prompt, "go");
  assert.equal(dispatched.length, 0);
  assert.equal(instance.submitting, false);
});

test("without a repository it cannot submit", () => {
  const { instance } = setup({ defaultRepository: null });
  assert.equal(instance.cannotSubmit, true);
});

test("cancel reports back to the page", () => {
  const { instance, dispatched } = setup();
  instance.cancel();
  assert.deepEqual(dispatched, [{ name: "new-session-cancelled", detail: undefined }]);
});

test("preselects the checked-out branch, and branches off the one chosen", async () => {
  const { instance, memory, tick } = setup();
  await flush();
  tick();
  assert.deepEqual(instance.localBranches.map((b) => b.name), ["main", "feat/feed"]);
  assert.deepEqual(instance.remoteBranches.map((b) => b.name), ["origin/main"]);
  assert.equal(instance.baseBranch, "main");

  const started = [];
  const realStart = memory.gateway.start;
  memory.gateway.start = async (req) => (started.push(req), realStart(req));
  instance.baseBranch = "feat/feed";
  assert.ok(instance.baseHint.includes("feat/feed"));
  await instance.submit();
  assert.equal(started[0].baseBranch, "feat/feed");
});

test("choosing another repository lists its branches", async () => {
  const { instance, tick } = setup();
  await flush();
  tick();
  instance.repositoryId = "r2";
  instance.repositoryChanged();
  await flush();
  tick();
  assert.deepEqual(instance.branches.map((b) => b.name), ["develop"]);
  assert.equal(instance.baseBranch, "develop");
});

test("when branches cannot be listed the session still starts, off the checkout", async () => {
  const { instance, dispatched } = setup({ branches: {} });
  await flush();
  assert.equal(instance.branchesFailed, true);
  assert.ok(instance.baseHint.includes("Could not list"));
  await instance.submit();
  assert.equal(dispatched[0].name, "session-created");
});
