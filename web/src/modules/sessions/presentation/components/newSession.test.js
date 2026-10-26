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

const DEBOUNCE = 10;
const pause = (ms = DEBOUNCE * 4) => new Promise((resolve) => setTimeout(resolve, ms));

function setup({ defaultRepository = "r1", repositories = { r1: "p1", r2: "p1" }, branches = BRANCHES } = {}) {
  const memory = memoryGateway({ projects: ["p1"], repositories, branches });
  const form = document.createElement("form");
  if (defaultRepository !== null) form.dataset.defaultRepository = defaultRepository;
  const mounted = mount(() => newSession({ gateway: memory.gateway, debounceMs: DEBOUNCE })("p1", "t1"), { el: form });
  mounted.instance.init();
  return { ...mounted, memory };
}

test("starts an interactive session on the page's task and reports it", async () => {
  const { instance, dispatched, memory } = setup();
  await flush();
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
  await flush();
  assert.equal(instance.mode, "", "the default mode runs the agent as it is");
  instance.mode = "architect";
  assert.ok(instance.promptPlaceholder.includes("architect starts from the task"));
  assert.ok(instance.modeHint.includes("task-architecture"));
  await instance.submit();
  assert.equal(dispatched[0].detail.session.mode, "architect");
});

test("implement mode asks which spec and starts in that mode", async () => {
  const { instance, dispatched } = setup();
  await flush();
  instance.mode = "implement";
  assert.ok(instance.implement);
  assert.ok(instance.promptPlaceholder.includes("Spec"), instance.promptPlaceholder);
  assert.ok(instance.modeHint.includes("task-implement-spec"), instance.modeHint);
  await instance.submit();
  assert.equal(dispatched[0].detail.session.mode, "implement");
});

test("design mode tells the agent to publish as it goes", async () => {
  const { instance, dispatched } = setup();
  await flush();
  instance.mode = "design";
  assert.ok(instance.design);
  assert.ok(instance.promptPlaceholder.includes("Design tab"), instance.promptPlaceholder);
  assert.ok(instance.modeHint.includes("publishes"), instance.modeHint);
  await instance.submit();
  assert.equal(dispatched[0].detail.session.mode, "design");
});

test("a refused start shows the coded error and keeps the draft", async () => {
  const { instance, dispatched } = setup({ repositories: { r1: "another-project" } });
  await flush();
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

test("no error row while the branches list", async () => {
  const { instance } = setup();
  await flush();
  assert.equal(instance.branchError, null);
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
  assert.equal(instance.branchError, "Could not list the branches.", "the combobox's error row");
  await instance.submit();
  assert.equal(dispatched[0].name, "session-created");
});

test("product mode interviews first, writes no code, and starts on the page's task as the feature", async () => {
  const { instance, dispatched } = setup();
  await flush();
  instance.mode = "product";
  assert.ok(instance.product);
  assert.ok(instance.promptPlaceholder.includes("interviews you"), instance.promptPlaceholder);
  assert.ok(instance.modeHint.includes("task-product"), instance.modeHint);
  assert.ok(instance.modeHint.includes("never writes code"), instance.modeHint);
  await instance.submit();
  assert.deepEqual([dispatched[0].detail.session.mode, dispatched[0].detail.session.ticketId], ["product", "t1"]);
});

test("with no task the form is locked to product mode and starts a feature brainstorm on the project", async () => {
  const memory = memoryGateway({ projects: ["p1"], repositories: { r1: "p1" }, branches: BRANCHES });
  const form = document.createElement("form");
  form.dataset.defaultRepository = "r1";
  const mounted = mount(() => newSession({ gateway: memory.gateway })("p1", "", "product"), { el: form });
  mounted.instance.init();
  const { instance, dispatched } = mounted;
  assert.equal(instance.mode, "product");
  assert.equal(instance.lockedMode, "product");
  assert.equal(instance.cannotSubmit, false);
  await instance.submit();
  const { session } = dispatched[0].detail;
  assert.deepEqual([session.ticketId, session.mode], ["", "product"]);
  instance.mode = "";
  assert.ok(instance.problem.includes("without a task"), "no other mode starts without a task");
  assert.equal(instance.cannotSubmit, true);
});

// ── the session branch's name ────────────────────────────────────────

/** Counts the checks the form puts to the server. */
function watchChecks(memory) {
  const asked = [];
  const real = memory.gateway.checkBranchName;
  memory.gateway.checkBranchName = async (query, signal) => (asked.push(query), real(query, signal));
  return asked;
}

test("the branch name is prefilled with the generated one and free", async () => {
  const { instance } = setup();
  assert.equal(instance.nameState, "checking", "Start waits for the first answer");
  assert.equal(instance.cannotSubmit, true);
  assert.equal(instance.problem, "Checking the branch name…");
  await flush();
  assert.ok(instance.branchName.startsWith("agent/t1-"), instance.branchName);
  assert.equal(instance.nameState, "generated");
  assert.equal(instance.cannotSubmit, false);
});

test("a name is checked once, after typing pauses", async () => {
  const { instance, memory } = setup();
  await flush();
  const asked = watchChecks(memory);
  for (const name of ["f", "fe", "feat/feed"]) {
    instance.branchName = name;
    instance.nameTyped();
  }
  assert.equal(instance.nameState, "checking");
  assert.equal(instance.cannotSubmit, true);
  await pause();
  assert.deepEqual(asked.map((q) => q.name), ["feat/feed"], "one request, for the last name");
  assert.deepEqual([asked[0].repositoryId, asked[0].ticketId, asked[0].baseBranch], ["r1", "t1", ""]);
  assert.equal(instance.nameState, "taken");
});

test("a taken name says so, offers the free one in a chip, and blocks Start with a reason", async () => {
  const { instance } = setup();
  await flush();
  instance.branchName = "feat/feed";
  instance.nameTyped();
  await pause();
  assert.equal(instance.nameMessage, "feat/feed already exists on this repository. An existing branch is never reused.");
  assert.equal(instance.nameSuggestion, "feat/feed-2");
  assert.equal(instance.cannotSubmit, true);
  assert.equal(instance.problem, "The branch name is taken. Pick another.");

  instance.useSuggestion();
  await flush();
  assert.equal(instance.branchName, "feat/feed-2");
  assert.equal(instance.nameMessage, "Free on this repository");
  assert.equal(instance.cannotSubmit, false);
});

test("an invalid name gives the server's reason and blocks Start", async () => {
  const { instance } = setup();
  await flush();
  instance.branchName = "fix login";
  instance.nameTyped();
  await pause();
  assert.equal(instance.nameState, "invalid");
  assert.ok(instance.nameMessage.startsWith("Not a valid branch name: a branch name cannot contain spaces"), instance.nameMessage);
  assert.equal(instance.nameSuggestion, "");
  assert.equal(instance.cannotSubmit, true);
});

test("changing the repository or the base checks the name again", async () => {
  const { instance, memory, set } = setup();
  await flush();
  instance.branchName = "develop";
  instance.nameTyped();
  await pause();
  assert.equal(instance.nameState, "free", "r1 has no branch called develop");
  const asked = watchChecks(memory);

  instance.repositoryId = "r2";
  instance.repositoryChanged();
  await flush();
  assert.equal(asked.at(-1).repositoryId, "r2");
  assert.equal(instance.nameState, "taken", "r2 has a branch called develop");
  assert.equal(instance.branchName, "develop", "a name the person typed stays");

  const before = asked.length;
  set("baseBranch", "feat/feed");
  await pause();
  assert.equal(asked.length, before + 1);
  assert.equal(asked.at(-1).baseBranch, "feat/feed");
});

test("an answer to an older question is dropped", async () => {
  const { instance, memory } = setup();
  await flush();
  const real = memory.gateway.checkBranchName;
  let releaseFirst;
  let calls = 0;
  memory.gateway.checkBranchName = (query) => (++calls === 1 ? new Promise((r) => (releaseFirst = () => r(real(query)))) : real(query));
  instance.branchName = "feat/feed";
  instance.checkNameNow();
  instance.branchName = "fresh";
  await instance.checkNameNow();
  releaseFirst();
  await flush();
  assert.equal(instance.nameCheck.name, "fresh");
  assert.equal(instance.nameState, "free");
});

test("the session starts on the typed name, and on none when the field is emptied", async () => {
  const { instance, memory } = setup();
  await flush();
  const started = [];
  const realStart = memory.gateway.start;
  memory.gateway.start = async (req) => (started.push(req), realStart(req));
  instance.branchName = "  fix/login ";
  instance.nameTyped();
  await pause();
  await instance.submit();
  assert.equal(started[0].branchName, "fix/login");
  instance.branchName = "";
  instance.nameTyped();
  await pause();
  assert.equal(instance.cannotSubmit, false, "an empty name lets the server generate one");
  await instance.submit();
  assert.equal(started[1].branchName, "");
});

test("a start refused for the name shows on the field and keeps what was typed", async () => {
  const { instance, memory } = setup();
  await flush();
  instance.prompt = "go";
  instance.branchName = "feat/feed";
  instance.nameTyped();
  await pause();
  // The name was taken since the check: the form was told it was free.
  const realCheck = memory.gateway.checkBranchName;
  memory.gateway.checkBranchName = async (q) => ({ ...(await realCheck(q)), available: true, code: "", reason: "", suggestion: "" });
  instance.nameTyped();
  await pause();
  assert.equal(instance.nameState, "free");

  await instance.submit();
  assert.equal(instance.error, null, "no banner: the field says it");
  assert.equal(instance.nameState, "taken");
  assert.equal(instance.nameMessage, "feat/feed already exists on this repository. An existing branch is never reused.");
  assert.equal(instance.nameSuggestion, "feat/feed-2");
  assert.deepEqual([instance.prompt, instance.branchName], ["go", "feat/feed"]);
  assert.equal(instance.submitting, false);
});

test("an invalid name refused at start shows the reason on the field", async () => {
  const { instance, memory } = setup();
  await flush();
  memory.gateway.checkBranchName = async (q) => ({ name: q.name, generated: "agent/x", valid: true, available: true, code: "", reason: "", suggestion: "" });
  instance.branchName = "no way";
  instance.nameTyped();
  await pause();
  await instance.submit();
  assert.equal(instance.error, null);
  assert.equal(instance.nameState, "invalid");
  assert.ok(instance.nameMessage.includes("cannot contain spaces"), instance.nameMessage);
});

test("when the name cannot be checked the form says so and still starts", async () => {
  const { instance, memory, dispatched } = setup();
  memory.gateway.checkBranchName = async () => {
    throw new Error("down");
  };
  instance.branchName = "x/y";
  instance.nameTyped();
  await pause();
  assert.equal(instance.nameState, "unchecked");
  assert.equal(instance.cannotSubmit, false);
  await instance.submit();
  assert.equal(dispatched[0].name, "session-created");
});
