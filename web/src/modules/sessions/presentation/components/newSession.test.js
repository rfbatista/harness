import { Codes, StructuredError } from "../../../../shared/domain/errors.js";
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

// ── the agent picker: enabled agents only (ADR 0038) ─────────────────────

const REVIEWER = { id: "reviewer", name: "reviewer", description: "Reviews code." };
const DESIGNER = { id: "designer", name: "designer", description: "" };

function pickerSetup({ agents = [REVIEWER], total = 3, mode = "selected", search = "" } = {}) {
  const memory = memoryGateway({ projects: ["p1"], repositories: { r1: "p1" }, branches: BRANCHES });
  memory.setAgentChoices("p1", { mode, agents, total });
  const form = document.createElement("form");
  form.dataset.defaultRepository = "r1";
  form.dataset.agents = JSON.stringify(agents);
  form.dataset.agentsTotal = String(total);
  form.dataset.agentsMode = mode;
  form.dataset.projectName = "atlas-api";
  const mounted = mount(() => newSession({ gateway: memory.gateway, debounceMs: DEBOUNCE, location: { search } })("p1", "t1"), { el: form });
  mounted.instance.init();
  return { ...mounted, memory };
}

test("a link's ?agent=<name> chooses that agent when the project enables it; any other name leaves Plain claude", async () => {
  const asked = pickerSetup({ agents: [REVIEWER, DESIGNER], search: "?agent=designer" });
  await flush();
  assert.equal(asked.instance.agentId, "designer");
  assert.equal(asked.instance.chosenAgentName, "designer");
  const unknown = pickerSetup({ agents: [REVIEWER], search: "?agent=designer" });
  await flush();
  assert.equal(unknown.instance.agentId, "", "an agent the project does not enable is ignored");
  const none = pickerSetup({ agents: [REVIEWER] });
  await flush();
  assert.equal(none.instance.agentId, "");
});

test("a wanted agent that the project enables only after the list refreshes is chosen then, once", async () => {
  const { instance, memory } = pickerSetup({ agents: [REVIEWER], search: "?agent=designer" });
  await flush();
  assert.equal(instance.agentId, "");
  memory.setAgentChoices("p1", { mode: "selected", agents: [REVIEWER, DESIGNER], total: 3 });
  await instance.refreshAgents();
  assert.equal(instance.agentId, "designer");
  instance.agentId = "";
  memory.setAgentChoices("p1", { mode: "selected", agents: [REVIEWER, DESIGNER], total: 3 });
  await instance.refreshAgents();
  assert.equal(instance.agentId, "", "the person's own choice is not overridden");
});

test("the picker offers the project's enabled agents, with the count and the hint", async () => {
  const { instance } = pickerSetup({ agents: [REVIEWER, DESIGNER], total: 10 });
  await flush();
  assert.deepEqual(instance.agentChoices.map((a) => a.name), ["reviewer", "designer"]);
  assert.equal(instance.agentCount, "2 enabled");
  assert.equal(instance.agentHint, "Showing the 2 agents enabled in atlas-api of 10 in the catalog.");
  assert.equal(instance.agentsSettingsLink, "/projects/p1/settings#settings-agents-enabled");
  assert.equal(instance.noAgentEnabled, false);
});

test("an All-agents project says new agents appear automatically", async () => {
  const { instance } = pickerSetup({ agents: [REVIEWER, DESIGNER], total: 2, mode: "all" });
  assert.ok(instance.agentHint.includes("new agents appear automatically"), instance.agentHint);
});

test("an empty Selected list replaces the picker with the warning and turns Start off", async () => {
  const { instance, dispatched } = pickerSetup({ agents: [], total: 3 });
  await flush();
  assert.equal(instance.noAgentEnabled, true);
  assert.equal(instance.cannotSubmit, true);
  assert.equal(instance.problem, "No agent is enabled in atlas-api.");
  await instance.submit();
  assert.deepEqual(dispatched, [], "nothing was started");
});

test("the picker follows agent_catalog_changed for its project while the form is open", async () => {
  const { instance, memory } = pickerSetup({ agents: [REVIEWER], total: 3 });
  await flush();
  memory.setAgentChoices("p1", { mode: "selected", agents: [REVIEWER, DESIGNER], total: 3 });
  await flush();
  assert.deepEqual(instance.agentChoices.map((a) => a.name), ["reviewer", "designer"]);
  assert.equal(instance.agentCount, "2 enabled");

  memory.publishCatalogEvent({ agent_catalog_changed: { paths: ["projects/p2"] } });
  memory.setAgentChoices("p2", { mode: "selected", agents: [], total: 3 }); // not this project
  await flush();
  assert.equal(instance.agents.length, 2, "another project's list is none of its business");

  instance.destroy();
  memory.setAgentChoices("p1", { mode: "selected", agents: [], total: 3 });
  await flush();
  assert.equal(instance.agents.length, 2, "a closed form no longer follows");
});

test("a choice taken off the list while the form is open stays visible, marked, and Start is refused with the banner", async () => {
  const { instance, memory, dispatched } = pickerSetup({ agents: [REVIEWER, DESIGNER], total: 3 });
  await flush();
  instance.agentId = "designer";
  instance.prompt = "keep this";
  memory.setAgentChoices("p1", { mode: "selected", agents: [REVIEWER], total: 3 });
  await flush();
  assert.deepEqual(instance.agentChoices.map((a) => a.name), ["reviewer", "designer (no longer enabled)"]);

  await instance.submit();
  assert.deepEqual(dispatched, [], "no session started");
  assert.equal(instance.agentRefusal.message, "The session was not started: designer is not enabled in atlas-api.");
  assert.equal(instance.agentRefusal.code, "AGENT_NOT_ENABLED");
  assert.equal(instance.agentRefusal.linkText, "Open atlas-api's Agents settings");
  assert.equal(instance.agentId, "", "no agent is selected");
  assert.equal(instance.prompt, "keep this", "the form stays filled in");
  assert.equal(instance.error, null, "the refusal has its own banner");
  assert.deepEqual(instance.agentChoices.map((a) => a.name), ["reviewer"]);

  instance.pickAnotherAgent();
  assert.equal(instance.agentRefusal, null);
});

// ── the picker follows the repository (ADR 0061) ─────────────────────────

const OPS = { id: "ops", name: "ops", description: "Runs things.", repositoryPromptSet: true };

function repositoryPicker({ search = "" } = {}) {
  const memory = memoryGateway({ projects: ["p1"], repositories: { r1: "p1", r2: "p1" }, branches: BRANCHES });
  memory.setAgentChoices("p1", { mode: "selected", agents: [REVIEWER, DESIGNER], total: 5 });
  memory.setAgentChoices("p1", { mode: "selected", agents: [REVIEWER, DESIGNER], total: 5, notOffered: [] }, "r1");
  memory.setAgentChoices("p1", { mode: "selected", agents: [REVIEWER, OPS], total: 5, notOffered: [{ name: "designer", reason: "unassigned" }, { name: "qa", reason: "not_enabled" }] }, "r2");
  const form = document.createElement("form");
  form.dataset.defaultRepository = "r1";
  form.dataset.agents = JSON.stringify([REVIEWER, DESIGNER]);
  form.dataset.agentsTotal = "5";
  form.dataset.agentsMode = "selected";
  form.dataset.projectName = "atlas";
  form.dataset.projectId = "p1";
  form.dataset.repositories = JSON.stringify([{ id: "r1", name: "atlas-api" }, { id: "r2", name: "atlas-web" }]);
  const mounted = mount(() => newSession({ gateway: memory.gateway, debounceMs: DEBOUNCE, location: { search } })("p1", "t1"), { el: form });
  mounted.instance.init();
  return { ...mounted, memory };
}

test("another repository offers its own assigned agents, and the missing ones wait behind a disclosure with their reason", async () => {
  const { instance, set } = repositoryPicker();
  await flush();
  assert.deepEqual(instance.agentChoices.map((a) => a.name), ["reviewer", "designer"]);
  assert.equal(instance.hasNotOffered, false);
  set("repositoryId", "r2");
  await flush();
  assert.deepEqual(instance.agentChoices.map((a) => a.name), ["reviewer", "ops"]);
  assert.equal(instance.agentCount, "2 assigned");
  assert.equal(instance.agentHintLink.href, "/projects/p1/repositories/r2/architecture/agents");
  assert.equal(instance.agentHintLink.text, "Manage agents in atlas-web");
  assert.equal(instance.agentHint, "Showing the 2 agents assigned to atlas-web, of 3 enabled in atlas. 1 is unassigned here.");
  assert.equal(instance.notOfferedTitle, "Not offered in atlas-web (2)");
  assert.deepEqual(instance.notOfferedList.map((r) => [r.name, r.reason, r.href]), [
    ["designer", "unassigned from atlas-web", "/projects/p1/repositories/r2/architecture/agents"],
    ["qa", "not enabled in atlas", "/projects/p1/settings#settings-agents-enabled"],
  ]);
});

test("a chosen agent the new repository does not offer is dropped, with the reason; the prompt typed is kept", async () => {
  const { instance, set } = repositoryPicker();
  await flush();
  instance.agentId = "designer";
  instance.prompt = "keep this";
  set("repositoryId", "r2");
  await flush();
  assert.equal(instance.agentId, "");
  assert.equal(instance.agentNote, "designer is not assigned to atlas-web, so the agent is back on Plain claude.");
  assert.equal(instance.prompt, "keep this");
  set("repositoryId", "r1");
  await flush();
  assert.equal(instance.agentNote, "", "the note goes with the next read");
});

test("an agent with a repository prompt is marked in the picker", async () => {
  const { instance, set } = repositoryPicker();
  set("repositoryId", "r2");
  await flush();
  assert.deepEqual(instance.agentOptions.map((a) => [a.name, a.description]), [["reviewer", "Reviews code."], ["ops", "Runs things. · repository prompt set"]]);
});

test("the picker follows repository_agents_changed for the chosen repository while the form is open", async () => {
  const { instance, memory } = repositoryPicker();
  await flush();
  memory.setAgentChoices("p1", { mode: "selected", agents: [REVIEWER], total: 5, notOffered: [{ name: "designer", reason: "unassigned" }] }, "r1");
  await flush();
  assert.deepEqual(instance.agentChoices.map((a) => a.name), ["reviewer"]);
  assert.equal(instance.notOfferedList.length, 1);
});

test("a start refused AGENT_NOT_ASSIGNED shows the banner, drops the agent and keeps the prompt", async () => {
  const { instance, memory, dispatched } = repositoryPicker();
  await flush();
  instance.agentId = "designer";
  instance.prompt = "keep this";
  memory.setUnassigned("r1", ["designer"]);
  await instance.submit();
  assert.deepEqual(dispatched, [], "no session started");
  assert.equal(instance.agentRefusal.code, "AGENT_NOT_ASSIGNED");
  assert.equal(instance.agentRefusal.message, "The session was not started: designer is not assigned to r1.");
  assert.equal(instance.agentRefusal.href, "/projects/p1/repositories/r1/architecture/agents");
  assert.equal(instance.agentId, "");
  assert.equal(instance.prompt, "keep this");
  assert.equal(instance.error, null, "the refusal has its own banner");
});

// ── Fetch from origin ────────────────────────────────────────────────

const remoteNames = (instance) => instance.remoteBranches.map((b) => b.name);

test("opening the form and changing the repository fetch nothing", async () => {
  const { instance, memory } = setup();
  await flush();
  instance.repositoryId = "r2";
  instance.repositoryChanged();
  await flush();
  assert.equal(memory.fetches.length, 0);
});

test("Fetch from origin fetches, then lists again: a pushed branch appears, a pruned one leaves", async () => {
  const { instance, memory } = setup();
  await flush();
  memory.setOrigin("r1", ["origin/main", "origin/feature-x"]);
  assert.deepEqual(remoteNames(instance), ["origin/main"], "the list is as stale as the checkout");
  await instance.fetchOrigin();
  assert.deepEqual(memory.fetches, ["r1"]);
  assert.deepEqual(remoteNames(instance), ["origin/feature-x", "origin/main"]);
  memory.setOrigin("r1", ["origin/main"]);
  await instance.fetchOrigin();
  assert.deepEqual(remoteNames(instance), ["origin/main"]);
});

test("a fetch keeps the pick when its branch still exists", async () => {
  const { instance, memory } = setup();
  await flush();
  instance.baseBranch = "feat/feed";
  memory.setOrigin("r1", ["origin/main", "origin/feature-x"]);
  await instance.fetchOrigin();
  await flush();
  assert.equal(instance.baseBranch, "feat/feed");
  assert.equal(instance.fetchNote, "");
});

test("a fetch that prunes the pick falls back to the checked-out branch and says so", async () => {
  const { instance, memory } = setup();
  await flush();
  memory.setOrigin("r1", ["origin/main", "origin/feature-x"]);
  await instance.fetchOrigin();
  instance.baseBranch = "origin/feature-x";
  memory.setOrigin("r1", ["origin/main"]);
  await instance.fetchOrigin();
  await flush();
  assert.equal(instance.baseBranch, "main");
  assert.equal(instance.fetchNote, "origin/feature-x is not on origin any more; the field is back on the checked-out branch.");
  assert.equal(instance.baseHint.startsWith("origin/feature-x is not on origin any more"), true, "the hint carries the note");
});

test("while the fetch runs the form is busy and cannot start, and it is free after", async () => {
  const { instance, memory } = setup();
  await flush();
  const gate = memory.holdFetch();
  const running = instance.fetchOrigin();
  assert.equal(instance.fetching, true);
  assert.equal(instance.branchBusy, true, "the combobox's aria-busy");
  assert.equal(instance.baseHint, "Fetching from origin…");
  assert.equal(instance.cannotSubmit, true);
  gate.release();
  await running;
  assert.equal(instance.fetching, false);
  assert.equal(instance.branchBusy, false);
  assert.equal(instance.cannotSubmit, false);
});

test("a failed fetch says why in the error row, keeps the list, and Retry fetches again", async () => {
  const { instance, memory } = setup();
  await flush();
  instance.baseBranch = "feat/feed";
  for (const code of [Codes.NO_REMOTE, Codes.GIT_CREDENTIAL_MISSING, Codes.GIT_CREDENTIAL_REJECTED, Codes.GIT_CREDENTIAL_STORE_LOCKED, Codes.FETCH_FAILED]) {
    memory.failFetch(new StructuredError(code, `sentence for ${code}`, 502));
    await instance.fetchOrigin();
    assert.equal(instance.branchError, `sentence for ${code}`);
    assert.equal(instance.baseStatus, `sentence for ${code}`, "and on the status line, where a closed field shows it");
    assert.equal(instance.baseBranch, "feat/feed");
    assert.deepEqual(remoteNames(instance), ["origin/main"], "the stale list stays");
    assert.equal(instance.cannotSubmit, false, "starting from a local branch still works");
    assert.equal(instance.fetching, false);
  }
  memory.failFetch(null);
  memory.setOrigin("r1", ["origin/main", "origin/feature-x"]);
  await instance.fetchOrigin();
  assert.equal(instance.branchError, null);
  assert.deepEqual(remoteNames(instance), ["origin/feature-x", "origin/main"]);
});

test("a repository change during a fetch drops the fetch's result", async () => {
  const { instance, memory } = setup();
  await flush();
  const gate = memory.holdFetch();
  const running = instance.fetchOrigin();
  instance.repositoryId = "r2";
  instance.repositoryChanged();
  await flush();
  memory.setOrigin("r1", ["origin/gone"]);
  gate.release();
  await running;
  await flush();
  assert.deepEqual(instance.branches.map((b) => b.name), ["develop"], "r2's list is untouched");
  assert.equal(instance.fetching, false);
  assert.equal(instance.branchError, null);
});

test("a failed fetch for a superseded repository leaves no error on the new one", async () => {
  const { instance, memory } = setup();
  await flush();
  const gate = memory.holdFetch();
  memory.failFetch(new StructuredError(Codes.FETCH_FAILED, "boom", 502));
  const running = instance.fetchOrigin();
  instance.repositoryId = "r2";
  instance.repositoryChanged();
  await flush();
  gate.release();
  await running;
  assert.equal(instance.branchError, null);
});

// ── a remote base ────────────────────────────────────────────────────

const WITH_REMOTES = {
  r1: [
    { name: "main", remote: false, isHead: true },
    { name: "origin/feature-x", remote: true, isHead: false },
    { name: "upstream/x", remote: true, isHead: false },
  ],
};

test("a remote base is explained: cut from origin/<name>, fetched from origin when the session starts", async () => {
  const { instance } = setup({ branches: WITH_REMOTES });
  await flush();
  instance.baseBranch = "origin/feature-x";
  assert.equal(instance.baseHint, "The session gets its own branch, cut from origin/feature-x; it is fetched from origin when the session starts.");
  assert.equal(instance.nameHint, "Becomes the worktree's branch. Cut from origin/feature-x.");
});

test("a local base, the checked-out branch and another remote's ref keep the plain hint", async () => {
  const { instance } = setup({ branches: WITH_REMOTES });
  await flush();
  assert.equal(instance.baseHint, "The session gets its own branch, cut from the branch checked out in the repository.");
  instance.baseBranch = "upstream/x";
  assert.equal(instance.baseHint, "The session gets its own branch, cut from upstream/x.");
});

test("a remote base is sent to the server as origin/<name>", async () => {
  const { instance, memory } = setup({ branches: WITH_REMOTES });
  await flush();
  const started = [];
  const realStart = memory.gateway.start;
  memory.gateway.start = async (req) => (started.push(req), realStart(req));
  instance.baseBranch = "origin/feature-x";
  await instance.submit();
  assert.equal(started[0].baseBranch, "origin/feature-x");
});

// ── a start refused for the base ─────────────────────────────────────

const BASE_CODES = [
  Codes.BASE_BRANCH_NOT_FOUND,
  Codes.REMOTE_BRANCH_NOT_FOUND,
  Codes.NO_REMOTE,
  Codes.GIT_CREDENTIAL_MISSING,
  Codes.GIT_CREDENTIAL_REJECTED,
  Codes.GIT_CREDENTIAL_STORE_LOCKED,
  Codes.FETCH_FAILED,
];

test("a start refused for the base shows the server's sentence under Branch off and keeps the form as filled", async () => {
  for (const code of BASE_CODES) {
    const { instance, memory, dispatched } = setup({ branches: WITH_REMOTES });
    await flush();
    instance.prompt = "do the thing";
    instance.agentId = "a-reviewer";
    instance.mode = "implement";
    instance.baseBranch = "origin/feature-x";
    instance.branchName = "agent/mine";
    instance.branchNameEdited = true;
    memory.refuseStart(new StructuredError(code, `sentence for ${code}`, 404));
    await instance.submit();
    assert.equal(instance.baseStatus, `sentence for ${code}`, code);
    assert.equal(instance.baseInvalid, true, code);
    assert.equal(instance.error, null, `${code}: not in the banner`);
    assert.equal(instance.submitting, false);
    assert.equal(instance.cannotSubmit, false, `${code}: one fix and one click`);
    assert.deepEqual([instance.prompt, instance.agentId, instance.mode, instance.baseBranch, instance.branchName], ["do the thing", "a-reviewer", "implement", "origin/feature-x", "agent/mine"]);
    assert.equal(dispatched.length, 0);
  }
});

test("the base refusal goes when the person picks another branch, and a fresh start succeeds", async () => {
  const { instance, memory, dispatched } = setup({ branches: WITH_REMOTES });
  await flush();
  instance.baseBranch = "origin/feature-x";
  memory.refuseStart(new StructuredError(Codes.REMOTE_BRANCH_NOT_FOUND, "origin/feature-x is not on origin any more. Fetch from origin to refresh the list.", 404));
  await instance.submit();
  assert.equal(instance.baseInvalid, true);
  instance.baseBranch = "main";
  assert.equal(instance.baseInvalid, false);
  assert.equal(instance.baseStatus, "");
  await instance.submit();
  assert.equal(dispatched[0].name, "session-created");
});

test("the base refusal goes when the person fetches from origin", async () => {
  const { instance, memory } = setup({ branches: WITH_REMOTES });
  await flush();
  instance.baseBranch = "origin/feature-x";
  memory.refuseStart(new StructuredError(Codes.REMOTE_BRANCH_NOT_FOUND, "gone", 404));
  await instance.submit();
  await instance.fetchOrigin();
  assert.equal(instance.baseInvalid, false);
  assert.equal(instance.baseStatus, "");
});

test("refusals about the name and the agent keep their places, and any other code keeps the banner", async () => {
  const { instance, memory } = setup({ branches: WITH_REMOTES });
  await flush();
  memory.refuseStart(new StructuredError(Codes.INVALID_INPUT, "bad", 400));
  await instance.submit();
  assert.ok(instance.error);
  assert.equal(instance.baseInvalid, false);
  memory.refuseStart(new StructuredError(Codes.BRANCH_EXISTS, "BRANCH_EXISTS: x already exists", 409, { name: "x", suggestion: "x-2" }));
  await instance.submit();
  assert.equal(instance.nameState, "taken");
  assert.equal(instance.baseInvalid, false);
  assert.equal(instance.error, null);
});
